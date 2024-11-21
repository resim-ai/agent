package main

import (
	"bytes"
	"context"
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
	"github.com/resim-ai/agent/api"
	"github.com/spf13/viper"
	"golang.org/x/oauth2"
)

const agentVersion = "v0.2.4"

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
}

type Agent struct {
	APIClient         *api.ClientWithResponses
	Docker            DockerClient
	CurrentToken      *oauth2.Token
	TokenMutex        sync.Mutex
	ClientID          string
	AuthHost          string
	APIHost           string
	Name              string
	PoolLabels        []string
	ConfigDirOverride string
	LogDirOverride    string
	LogLevel          string
	Status            agentStatus
	CurrentTaskName   string
	CurrentTaskStatus api.TaskStatus
	AutoUpdate        bool
	Privileged        bool
	NetworkMode       DockerNetworkMode
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
				a.updateTaskStatus(ctx, taskStatusMessage.Name, taskStatusMessage.Status)
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

		taskStateChan <- taskStatusMessage{
			Name: *task.TaskName,
			// TODO: set this back to STARTING
			Status: "SUBMITTED",
		}
		agentStateChan <- agentStatusRunning
		a.pullImage(ctx, *task.WorkerImageURI)

		err := a.runWorker(ctx, Task(task), taskStateChan)
		if err != nil {
			slog.Error("Error running worker", "err", err)
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

func (a *Agent) runWorker(ctx context.Context, task Task, taskStateChan chan taskStatusMessage) error {
	providedEnvVars := StringifyEnvironmentVariables(*task.WorkerEnvironmentVariables)
	extraEnvVars := []string{
		"RERUN_WORKER_ENVIRONMENT=dev",
		fmt.Sprintf("RERUN_WORKER_DOCKER_NETWORK_MODE=%v", a.NetworkMode),
	}
	if a.Privileged {
		extraEnvVars = append(extraEnvVars, "RERUN_WORKER_PRIVILEGED=true")
	}

	var homeDir string
	user, err := user.Current()
	if err != nil {
		slog.Warn("Couldn't lookup user; assuming root", "error", err)
		homeDir = "/root"
	} else {
		homeDir = user.HomeDir
	}
	hostDockerConfigDir, _ := filepath.Abs(filepath.Join(homeDir, ".docker"))

	config := &container.Config{
		Image: *task.WorkerImageURI,
		Env:   append(providedEnvVars, extraEnvVars...),
	}

	res, err := a.Docker.ContainerCreate(
		context.TODO(),
		config,
		&container.HostConfig{
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
		},
		&network.NetworkingConfig{},
		&v1.Platform{},
		*task.TaskName,
	)
	if err != nil {
		fmt.Println(err)
	}

	err = a.Docker.ContainerStart(ctx, res.ID, container.StartOptions{})
	if err != nil {
		return err
	}
	slog.Info("Container for task starting", "task", *task.TaskName)
	taskStateChan <- taskStatusMessage{
		Name:   *task.TaskName,
		Status: api.RUNNING,
	}
	a.setCurrentTask(*task.TaskName, api.RUNNING)
	for {
		status, err := a.Docker.ContainerInspect(ctx, res.ID)
		if err != nil {
			return err
		}
		if status.State.Status != "running" {
			if status.State.ExitCode == 0 {
				slog.Info("Container for task succeeded", "task", *task.TaskName)
				taskStateChan <- taskStatusMessage{
					Name:   *task.TaskName,
					Status: api.SUCCEEDED,
				}
			} else {
				slog.Info("Container exited non-zero", "task", *task.TaskName, "exit_code", status.State.ExitCode, "err", status.State.Error)
				taskStateChan <- taskStatusMessage{
					Name:   *task.TaskName,
					Status: api.ERROR,
				}
			}
			a.setCurrentTask("", "")
			break
		} else {
			slog.Info("Container is running", "task", *task.TaskName)
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func (a *Agent) startHeartbeat(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)

	hbInput := api.AgentHeartbeatInput{
		AgentName:  &a.Name,
		PoolLabels: &a.PoolLabels,
	}

	go func() {
		for range ticker.C {
			if a.CurrentTaskName != "" {
				hbInput.TaskName = &a.CurrentTaskName
			}
			if a.CurrentTaskStatus != "" {
				hbInput.TaskStatus = &a.CurrentTaskStatus
			}

			_, err := a.APIClient.AgentHeartbeat(ctx, hbInput)
			if err != nil {
				log.Fatal(err)
			}
		}
	}()

	return nil
}

func (a *Agent) updateTaskStatus(ctx context.Context, taskName string, status api.TaskStatus) error {
	slog.Info("Updating task status", "task_name", taskName, "status", status)

	_, err := a.APIClient.UpdateTask(ctx, taskName, api.UpdateTaskInput{
		Status: &status,
	})

	return err
}

func (a *Agent) setCurrentTask(taskName string, status api.TaskStatus) {
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
