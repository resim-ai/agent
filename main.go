package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/resim-ai/agent/api"
	"github.com/spf13/viper"
	"golang.org/x/oauth2"
)

const agentVersion = "v0.6.0"

type agentStatus string

const (
	agentStatusIdle     agentStatus = "IDLE"
	agentStatusStarting agentStatus = "STARTING"
	agentStatusRunning  agentStatus = "RUNNING"
	agentStatusError    agentStatus = "ERROR"
)

type taskStatusMessage struct {
	Name   string
	Status api.TaskStatus
	Error  *api.ErrorType
}

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
	CurrentTaskName      string
	CurrentTaskStatus    api.TaskStatus
	AutoUpdate           bool
	Privileged           bool
	DockerNetworkMode    DockerNetworkMode
	HostAWSConfigDir     string
	HostAWSConfigExists  bool
	CustomerWorkerConfig CustomWorkerConfig
	// For testing purposes - allows mocking the AWS config directory lookup
	getAWSConfigDirFunc func() (string, bool)
}

type Task api.TaskPollOutput

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

	err = a.Start()
	if err != nil {
		os.Exit(1)
	} else {
		os.Exit(0)
	}
}

func New(dockerClient DockerClient) *Agent {
	return &Agent{
		Docker: dockerClient,
	}
}

func (a *Agent) Start() error {
	err := a.LoadConfig()
	if err != nil {
		slog.Error("error loading config", "err", err)
		return err
	}

	err = a.InitializeLogging()
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

	agentStateChan := make(chan agentStatus)
	taskStateChan := make(chan taskStatusMessage)

	go func() {
		for {
			select {
			case taskStatusMessage := <-taskStateChan:
				err := a.updateTaskStatus(ctx, taskStatusMessage.Name, taskStatusMessage.Status, taskStatusMessage.Error)
				if err != nil {
					slog.Error("Error updating task status", "err", err)
				}
			case agentStatus := <-agentStateChan:
				a.Status = agentStatus
			}
		}
	}()

	agentStateChan <- agentStatusIdle

	a.startHeartbeat(ctx)

	err = CreateTmpResimDir()
	if err != nil {
		slog.Error("Error creating /tmp/resim", "err", err)
		os.Exit(1)
	}

	for {
		task := a.getTask()
		if task.TaskName == nil {
			time.Sleep(10 * time.Second)
			continue
		}

		slog.Info("Got new task", "task_name", *task.TaskName)
		// Set the current task with the agent
		a.setCurrentTask(*task.TaskName, api.SUBMITTED)
		taskStateChan <- taskStatusMessage{
			Name:   *task.TaskName,
			Status: api.SUBMITTED,
		}
		agentStateChan <- agentStatusRunning

		// Attempt to pull the worker image; if this fails, we need to error
		// the task.
		err := a.pullImage(ctx, *task.WorkerImageURI)
		if err != nil {
			slog.Error("Error pulling image", "err", err)
			taskStateChan <- taskStatusMessage{
				Name:   *task.TaskName,
				Status: api.ERROR,
				Error:  Ptr(api.AGENTERRORPULLINGWORKERIMAGE),
			}
			a.setCurrentTask("", "")
			continue
		}

		// Attempt to run the worker; if this fails, we need to error the task.
		err = a.runWorker(ctx, Task(task))
		if err != nil {
			slog.Error("Error running worker", "err", err)
			taskStateChan <- taskStatusMessage{
				Name:   *task.TaskName,
				Status: api.ERROR,
				Error:  Ptr(api.AGENTERRORRUNNINGWORKER),
			}
			a.setCurrentTask("", "")
			continue
		}

		agentStateChan <- agentStatusIdle

		if viper.GetBool(OneTaskKey) {
			slog.Info("Agent launched in one-task mode, exiting")
			return nil
		}
	}
}

func (a *Agent) pullImage(ctx context.Context, targetImage string) error {
	slog.Info("Pulling image", "image", targetImage)
	r, err := a.Docker.ImagePull(ctx, targetImage, image.PullOptions{
		Platform: "linux/amd64",
	})
	if err != nil {
		return err
	}

	var buffer bytes.Buffer
	io.Copy(&buffer, r)
	r.Close()
	slog.Info("Pulled image", "image", targetImage)

	return nil
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

func (a *Agent) getTask() api.TaskPollOutput {
	ctx := context.Background()

	pollResponse, err := a.APIClient.TaskPollWithResponse(ctx, api.TaskPollInput{
		AgentID:    a.Name,
		PoolLabels: a.PoolLabels,
	})
	if err != nil {
		slog.Error("Error polling for task", "err", err)
	}

	switch pollResponse.StatusCode() {
	case 204:
		slog.Debug("No task available")
		return api.TaskPollOutput{}
	case 200:
		task := pollResponse.JSON200
		slog.Info("Received task", "task-name", *task.TaskName)
		return *task
	default:
		slog.Error("error polling for task", "err", pollResponse.StatusCode())
	}

	return api.TaskPollOutput{}
}

func StringifyEnvironmentVariables(inputVars [][]string) []string {
	var envVars []string
	for _, v := range inputVars {
		envVarString := fmt.Sprintf("%v=%v", v[0], v[1])
		envVars = append(envVars, envVarString)
	}
	return envVars
}

func (a *Agent) runWorker(ctx context.Context, task Task) error {
	providedEnvVars := StringifyEnvironmentVariables(*task.WorkerEnvironmentVariables)
	extraEnvVars := []string{
		"RERUN_WORKER_ENVIRONMENT=dev",
		fmt.Sprintf("RERUN_WORKER_DOCKER_NETWORK_MODE=%v", a.DockerNetworkMode),
		fmt.Sprintf("RERUN_WORKER_CONTAINER_TIMEOUT=%v", task.ContainerTimeout),
		fmt.Sprintf("RERUN_WORKER_WORKER_ID=%v", *task.TaskName), // This is the task name, which is the same as the worker ID for internal workloads.
	}
	if a.Privileged {
		extraEnvVars = append(extraEnvVars, "RERUN_WORKER_PRIVILEGED=true")
	}

	// convert the custom worker config to json string:
	customWorkerConfigJSON, err := json.Marshal(a.CustomerWorkerConfig)
	if err != nil {
		slog.Error("Error marshalling custom worker config", "err", err)
		return err
	}
	slog.Info("Custom worker config", "config", string(customWorkerConfigJSON))
	extraEnvVars = append(extraEnvVars, "RERUN_WORKER_CUSTOM_WORKER_CONFIG="+string(customWorkerConfigJSON))

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
		Image: *task.WorkerImageURI,
		Env:   append(providedEnvVars, extraEnvVars...),
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

	res, err := a.Docker.ContainerCreate(
		context.TODO(),
		config,
		hostConfig,
		&network.NetworkingConfig{},
		&v1.Platform{},
		*task.TaskName,
	)
	if err != nil {
		// Try to remove container and volumes if there is an error:
		a.removeContainer(ctx, res.ID)
		return errors.Wrapf(err, "error creating container for task %s", *task.TaskName)
	}

	err = a.Docker.ContainerStart(ctx, res.ID, container.StartOptions{})
	if err != nil {
		// Try to remove container and volumes if there is an error:
		a.removeContainer(ctx, res.ID)
		return errors.Wrapf(err, "error starting container for task %s", *task.TaskName)
	}
	slog.Info("Container for task starting", "task", *task.TaskName)
	// From now one, the worker is responsible for updating its own status.
	a.setCurrentTask(*task.TaskName, api.RUNNING)
	for {
		status, err := a.Docker.ContainerInspect(ctx, res.ID)
		if err != nil {
			a.setCurrentTask("", "")
			return errors.Wrapf(err, "error inspecting container for task %s", *task.TaskName)
		}
		if status.State.Status != "running" {
			if status.State.ExitCode == 0 {
				slog.Info("Container for task succeeded", "task", *task.TaskName)
			} else {
				slog.Info("Container exited non-zero", "task", *task.TaskName, "exit_code", status.State.ExitCode, "err", status.State.Error)
			}
			a.setCurrentTask("", "")
			break
		} else {
			slog.Info("Container is running", "task", *task.TaskName)
		}
		time.Sleep(2 * time.Second)
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

func (a *Agent) startHeartbeat(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)

	go func() {
		for range ticker.C {

			hbInput := api.AgentHeartbeatInput{
				AgentName:  &a.Name,
				PoolLabels: &a.PoolLabels,
			}

			// If we have a task, send the task name and status in the heartbeat,
			// taking care not to use a direct pointer to the values, as they may be
			// updated by the main loop, but not here.
			if a.CurrentTaskName != "" {
				currentTaskName := a.CurrentTaskName
				hbInput.TaskName = &currentTaskName
			}
			if a.CurrentTaskStatus != "" {
				currentTaskStatus := a.CurrentTaskStatus
				hbInput.TaskStatus = &currentTaskStatus
			}

			_, err := a.APIClient.AgentHeartbeat(ctx, hbInput)
			if err != nil {
				log.Fatal(err)
			}
		}
	}()

	return nil
}

func (a *Agent) updateTaskStatus(ctx context.Context, taskName string, status api.TaskStatus, error *api.ErrorType) error {
	slog.Info("Updating task status", "task_name", taskName, "status", status)

	updateTaskInput := api.UpdateTaskInput{
		Status: &status,
	}
	if error != nil {
		updateTaskInput.ErrorType = error
	}
	response, err := a.APIClient.UpdateTask(ctx, taskName, updateTaskInput)
	if err != nil {
		slog.Error("Error updating task status", "err", err)
		return err
	}

	if response.StatusCode != 200 {
		slog.Error("Received non-200 response from API when updating task status", "err", response.StatusCode)
		return fmt.Errorf("error updating task status: %v", response.Status)
	}

	slog.Info("Task status updated", "task_name", taskName, "status", status)
	return nil
}

func (a *Agent) setCurrentTask(taskName string, status api.TaskStatus) {
	slog.Info("Setting current task", "task_name", taskName, "status", status)
	a.CurrentTaskName = taskName
	a.CurrentTaskStatus = status
}

func CreateTmpResimDir() error {
	dir := "/tmp/resim"
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.Mkdir(dir, 0o700)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) getAPIClient(ctx context.Context) (*api.ClientWithResponses, error) {
	oauthClient := oauth2.NewClient(ctx, a)
	APIClient, err := api.NewClientWithResponses(a.APIHost, api.WithHTTPClient(oauthClient))
	if err != nil {
		return &api.ClientWithResponses{}, err
	}

	return APIClient, nil
}
