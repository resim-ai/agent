package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwt"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/resim-ai/agent/api"
	"github.com/spf13/viper"
	"golang.org/x/oauth2"
)

const agentVersion = "v1.1.0"

type agentStatus string

const (
	OrgIDClaim string = "https://api.resim.ai/org_id"
	TmpResim   string = "/tmp/resim"
)

type Agent struct {
	APIClient            *api.ClientWithResponses
	Docker               DockerClient
	CurrentToken         *oauth2.Token
	TokenMutex           sync.Mutex
	ClientID             string
	AuthHost             string
	APIHost              string
	Name                 string
	PoolLabels           []string
	ConfigDirOverride    string
	LogDirOverride       string
	LogLevel             string
	Status               agentStatus
	AutoUpdate           bool
	Privileged           bool
	DockerNetworkMode    DockerNetworkMode
	HostAWSConfigDir     string
	HostAWSConfigExists  bool
	CustomerWorkerConfig CustomWorkerConfig
	// For testing purposes - allows mocking the AWS config directory lookup
	getAWSConfigDirFunc    func() (string, bool)
	ImageMutex             sync.RWMutex
	WorkerImageURI         string
	CurrentErrorCount      int
	MaxErrorCount          int
	AgentErrorSleep        time.Duration // When the agent encounters an error, it will sleep for this duration before retrying
	WorkerExitSleep        time.Duration // After the worker exits, the agent will sleep for this duration before launching a new worker
	OrgName                string
	currentWorkerID        string
	ContainerWatchInterval time.Duration // How often to check the status of the container
	WorkerDir              string        // The directory to store the worker directory
	RemoveWorkerDir        bool          // Whether to remove the worker directory after the worker exits abnormally
	RemoveExperienceCache  bool          // Whether to remove the experience cache directory on agent exit
	ExperienceCacheDir     string        // The directory to store the experience cache
}

func main() {
	dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		slog.Error("error initializing Docker client", "err", err)
		os.Exit(1)
	}
	defer dockerClient.Close()

	a := New(dockerClient)

	ConfigDir := os.Getenv("RESIM_AGENT_CONFIG_DIR")
	if ConfigDir != "" {
		a.ConfigDirOverride = ConfigDir
	}

	LogDir := os.Getenv("RESIM_AGENT_LOG_DIR")
	if LogDir != "" {
		a.LogDirOverride = LogDir
	}

	err = a.LoadConfig()
	if err != nil {
		slog.Error("error loading config", "err", err)
	}

	err = a.Start()
	if a.RemoveExperienceCache {
		a.DeleteExperienceCache()
	}
	if err != nil {
		os.Exit(1)
	} else {
		os.Exit(0)
	}
}

func New(dockerClient DockerClient) *Agent {
	return &Agent{
		Docker:                 dockerClient,
		ContainerWatchInterval: 2 * time.Second,
		WorkerDir:              TmpResim,
	}
}

func (a *Agent) Start() error {
	a.getOrgName()

	err := a.InitializeLogging()
	if err != nil {
		slog.Error("error initializing logging", "err", err)
		return err
	}

	err = a.checkUpdate()
	if err != nil {
		slog.Error("error checking for update", "err", err)
		return err
	}

	ctx := context.Background()
	apiClient, err := a.getAPIClient(ctx)
	if err != nil {
		slog.Error("error setting API client", "err", err)
	}
	a.APIClient = apiClient
	defer a.saveCredentialCache()

	slog.Info("agent initialised", "version", agentVersion, "log_level", a.LogLevel)

	a.startHeartbeat()

	err = CreateDir(a.WorkerDir)
	if err != nil {
		slog.Error("Error creating /tmp/resim", "err", err)
		return err
	}

	// Create the experience cache directory if it doesn't exist
	err = CreateDir(a.ExperienceCacheDir)
	if err != nil {
		slog.Error("Error creating experience cache directory", "err", err, "path", a.ExperienceCacheDir)
		return err
	}

	var lastPulledImage string
	for {
		if a.CurrentErrorCount > a.MaxErrorCount {
			slog.Error("Agent has failed too many times in a row, exiting")
			if a.RemoveWorkerDir {
				_ = a.DeleteWorkerDir()
			}
			return err
		}

		var startup api.AgentCheckinOutput
		startup, err = a.checkin()
		slog.Info("Received startup response from AgentAPI")
		if err != nil {
			slog.Error("Error checking in", "err", err)
			err = errors.Wrap(err, fmt.Sprintf("error checking in (attempt %d)", a.CurrentErrorCount))
			a.CurrentErrorCount++
			time.Sleep(a.AgentErrorSleep)
			continue
		}
		if startup.WorkerImageURI == nil {
			slog.Info("Did not receive a worker image URI, sleeping for 60 seconds")
			err = errors.New(fmt.Sprintf("no worker image URI (attempt %d)", a.CurrentErrorCount))
			a.CurrentErrorCount++
			time.Sleep(a.AgentErrorSleep)
			continue
		}
		if startup.WorkerEnvironmentVariables == nil {
			slog.Error("No worker environment variables provided, cannot run worker")
			err = errors.New(fmt.Sprintf("no worker environment variables (attempt %d)", a.CurrentErrorCount))
			a.CurrentErrorCount++
			time.Sleep(a.AgentErrorSleep)
			continue
		}
		if startup.AuthToken == nil {
			slog.Error("No auth token provided, cannot run worker")
			err = errors.New(fmt.Sprintf("no auth token (attempt %d)", a.CurrentErrorCount))
			a.CurrentErrorCount++
			time.Sleep(a.AgentErrorSleep)
			continue
		}
		workerEnvVars := []string{}
		for _, envVar := range *startup.WorkerEnvironmentVariables {
			workerEnvVars = append(workerEnvVars, fmt.Sprintf("%s=%s", envVar[0], envVar[1]))
		}
		// Attempt to pull the worker image
		lastPulledImage, err = a.maybePullImage(ctx, lastPulledImage)
		if err != nil {
			slog.Error("Error pulling image", "err", err)
			err = errors.Wrap(err, fmt.Sprintf("error pulling image (attempt %d)", a.CurrentErrorCount))
			a.CurrentErrorCount++
			time.Sleep(a.AgentErrorSleep)
			continue
		}

		// Attempt to run the worker; if this fails, we need to error the task.
		err = a.runWorker(ctx, lastPulledImage, workerEnvVars)
		if err != nil {
			slog.Error("Error running ReSim worker", "err", err)
			err = errors.Wrap(err, fmt.Sprintf("error running ReSim worker (attempt %d)", a.CurrentErrorCount))
			a.CurrentErrorCount++
			time.Sleep(a.AgentErrorSleep)
			continue
		}

		a.CurrentErrorCount = 0
		if viper.GetBool(OneTaskKey) {
			slog.Info("Agent launched in one-task mode, exiting")
			return nil
		}
		time.Sleep(a.AgentErrorSleep)
	}
}

func (a *Agent) getOrgName() error {
	// decodes the org name from the token claim
	token, err := a.Token()
	if err != nil {
		slog.Error("Error getting token", "err", err)
		return err
	}
	claims, err := jwt.Parse([]byte(token.AccessToken), jwt.WithVerify(false))
	if err != nil {
		slog.Error("Error parsing token", "err", err)
		return err
	}
	orgName, _ := claims.Get(OrgIDClaim)
	if err != nil {
		slog.Error("Error getting org claim from token", "err", err)
		return err
	}
	if orgName == nil {
		slog.Error("No org claim in token")
		return errors.New("no org claim in token")
	}
	a.OrgName = orgName.(string)
	return nil
}

// The target image URI is recorded on the agent struct already.
// The URI passed in is the previous URI pulled. If the target image is different, it will be pulled.
// The return value is the last URI pulled - updated if the image was pulled.
func (a *Agent) maybePullImage(ctx context.Context, oldImage string) (string, error) {
	a.ImageMutex.RLock()
	defer a.ImageMutex.RUnlock()
	if a.WorkerImageURI == oldImage {
		slog.Info("Image already pulled", "image", oldImage)
		return oldImage, nil
	}

	slog.Info("Pulling image", "image", a.WorkerImageURI)
	r, err := a.Docker.ImagePull(ctx, a.WorkerImageURI, image.PullOptions{
		Platform: "linux/amd64",
	})
	if err != nil {
		return oldImage, err
	}

	var buffer bytes.Buffer
	io.Copy(&buffer, r)
	r.Close()
	slog.Info("Pulled image", "image", a.WorkerImageURI)

	return a.WorkerImageURI, nil
}

func (a *Agent) GetConfigDir() (string, error) {
	var expectedDir string
	if a.ConfigDirOverride != "" {
		expectedDir = a.ConfigDirOverride
	} else {
		expectedDir = os.ExpandEnv(ConfigPath)
	}
	// Check first if the directory exists, and if it does not, create it:
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		err := os.Mkdir(expectedDir, 0o700)
		if err != nil {
			slog.Error("Error creating directory")
			return "", err
		}
	}
	return expectedDir, nil
}

func (a *Agent) checkin() (api.AgentCheckinOutput, error) {
	ctx := context.Background()

	pollResponse, err := a.APIClient.AgentCheckinWithResponse(ctx, api.AgentCheckinInput{
		AgentID:      &a.Name,
		AgentVersion: Ptr(agentVersion),
		PoolLabels:   &a.PoolLabels,
	})
	if err != nil {
		slog.Error("Error checking in", "err", err)
		return api.AgentCheckinOutput{}, err
	}

	if pollResponse.StatusCode() != 200 {
		slog.Error("error polling for task", "err", pollResponse.StatusCode())
		return api.AgentCheckinOutput{}, errors.New("error polling for task")
	}

	a.ImageMutex.Lock()
	defer a.ImageMutex.Unlock()
	if pollResponse.JSON200.WorkerImageURI != nil {
		a.WorkerImageURI = *pollResponse.JSON200.WorkerImageURI
	}
	// TODO: handle forced agent update
	return *pollResponse.JSON200, nil
}

func StringifyEnvironmentVariables(inputVars [][]string) []string {
	var envVars []string
	for _, v := range inputVars {
		envVarString := fmt.Sprintf("%v=%v", v[0], v[1])
		envVars = append(envVars, envVarString)
	}
	return envVars
}

func (a *Agent) getWorkerID() string {
	return fmt.Sprintf("agent-%s|%s|%s", a.OrgName, a.Name, a.currentWorkerID)
}

func (a *Agent) runWorker(ctx context.Context, imageURI string, workerEnvVars []string) error {
	a.currentWorkerID = uuid.New().String() // assign a new workerID for tracking purposes every time
	providedEnvVars := []string{
		"RERUN_WORKER_ENVIRONMENT=dev",
		"RERUN_WORKER_REUSABLE=true",
		fmt.Sprintf("RERUN_WORKER_DOCKER_NETWORK_MODE=%v", a.DockerNetworkMode),
		fmt.Sprintf("RERUN_WORKER_WORKER_ID=%v", a.getWorkerID()),
	}
	if a.Privileged {
		providedEnvVars = append(providedEnvVars, "RERUN_WORKER_PRIVILEGED=true")
	}
	providedEnvVars = append(providedEnvVars, workerEnvVars...)
	providedEnvVars = append(providedEnvVars, fmt.Sprintf("RERUN_WORKER_POOL_LABELS=%v", strings.Join(a.PoolLabels, ",")))
	// convert the custom worker config to json string:
	customWorkerConfigJSON, err := json.Marshal(a.CustomerWorkerConfig)
	if err != nil {
		slog.Error("Error marshalling custom worker config", "err", err)
		return err
	}
	slog.Info("Custom worker config", "config", string(customWorkerConfigJSON))
	providedEnvVars = append(providedEnvVars, "RERUN_WORKER_CUSTOM_WORKER_CONFIG="+string(customWorkerConfigJSON))
	providedEnvVars = append(providedEnvVars, "RERUN_WORKER_WORKER_TYPE=agent")

	var homeDir string
	user, err := user.Current()
	if err != nil {
		slog.Warn("Couldn't lookup user; assuming root", "error", err)
		homeDir = "/root"
	} else {
		homeDir = user.HomeDir
	}

	hostDockerConfigDir, _ := filepath.Abs(filepath.Join(homeDir, ".docker"))
	_, err = os.Stat(hostDockerConfigDir)
	if err != nil {
		slog.Info("Docker config directory does not exist")
	}

	config := &container.Config{
		Image: imageURI,
		Env:   providedEnvVars,
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: "/var/run/docker.sock",
				Target: "/var/run/docker.sock",
			},
			{
				Type:   mount.TypeBind,
				Source: "/tmp/resim",
				Target: "/tmp/resim",
			},
			{
				Type:   mount.TypeBind,
				Source: hostDockerConfigDir,
				Target: "/root/.docker",
			},
		},
	}

	if a.HostAWSConfigExists {
		hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: a.HostAWSConfigDir,
			Target: "/root/.aws",
		})
	}

	// Mount the experience cache directory
	hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
		Type:   mount.TypeBind,
		Source: a.ExperienceCacheDir,
		Target: "/tmp/resim/cache",
	})

	res, err := a.Docker.ContainerCreate(
		context.TODO(),
		config,
		hostConfig,
		&network.NetworkingConfig{},
		&v1.Platform{},
		fmt.Sprintf("worker-%s", a.currentWorkerID),
	)
	if err != nil {
		// Try to remove container and volumes if there is an error:
		a.removeContainer(ctx, res.ID)
		return errors.Wrap(err, "error creating container for worker")
	}

	err = a.Docker.ContainerStart(ctx, res.ID, container.StartOptions{})
	if err != nil {
		// Try to remove container and volumes if there is an error:
		a.removeContainer(ctx, res.ID)
		return errors.Wrap(err, "error starting container for worker")
	}
	slog.Info("Container for worker starting", "worker", a.currentWorkerID)
	// From now one, the worker is responsible for updating its own status.
	for {
		status, err := a.Docker.ContainerInspect(ctx, res.ID)
		if err != nil {
			return errors.Wrap(err, "error inspecting container for worker")
		}
		if status.State.Status != "running" {
			if status.State.ExitCode == 0 {
				slog.Info("Worker succeeded")
			} else {
				slog.Info("Worker container exited non-zero", "exit_code", status.State.ExitCode, "err", status.State.Error)

			}
			time.Sleep(a.WorkerExitSleep)
			break
		} else {
			slog.Info("Worker is running")
		}
		time.Sleep(a.ContainerWatchInterval)
	}

	// Remove container and volumes:
	a.removeContainer(ctx, res.ID)

	return nil
}

func (a *Agent) removeContainer(ctx context.Context, containerID string) {
	err := a.Docker.ContainerRemove(ctx, containerID, container.RemoveOptions{
		RemoveVolumes: true,
	})
	if err != nil {
		slog.WarnContext(ctx, "error removing container", "error", err)
	}
}
func Ptr[T any](t T) *T {
	return &t
}

func (a *Agent) startHeartbeat() error {
	ticker := time.NewTicker(60 * time.Second)

	go func() {
		for range ticker.C {
			a.checkin()
		}
	}()

	return nil
}

func CreateDir(dir string) error {
	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		return err
	}
	return nil
}

func (a *Agent) DeleteWorkerDir() error {
	// Delete each directory in the worker directory, recursively:
	subpaths, err := os.ReadDir(a.WorkerDir)
	if err != nil {
		return err
	}
	for _, subpath := range subpaths {
		if subpath.Name() == "cache" {
			continue
		}
		removePath := filepath.Join(a.WorkerDir, subpath.Name())
		err = os.RemoveAll(removePath)
		if err != nil {
			slog.Warn("Error while deleting worker directory", "error", err, "path", removePath)
		}
	}
	return err
}

func (a *Agent) DeleteExperienceCache() {
	err := os.RemoveAll(a.ExperienceCacheDir)
	if err != nil {
		slog.Warn("Error while deleting experience cache", "error", err, "path", a.ExperienceCacheDir)
	}
}

func (a *Agent) getAPIClient(ctx context.Context) (*api.ClientWithResponses, error) {
	oauthClient := oauth2.NewClient(ctx, a)
	APIClient, err := api.NewClientWithResponses(
		a.APIHost,
		api.WithHTTPClient(oauthClient),
		api.WithRequestEditorFn(AddAgentIDEditor(a.Name, agentVersion)),
	)
	if err != nil {
		return &api.ClientWithResponses{}, err
	}

	return APIClient, nil
}

func AddAgentIDEditor(agentID string, agentVersion string) api.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set("X-ReSim-AgentID", agentID)
		req.Header.Set("X-ReSim-AgentVersion", agentVersion)
		return nil
	}
}
