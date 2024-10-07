package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
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

type agentStatus string

const (
	agentStatusIdle     agentStatus = "IDLE"
	agentStatusStarting agentStatus = "STARTING"
	agentStatusRunning  agentStatus = "RUNNING"
	agentStatusError    agentStatus = "ERROR"
)

// type taskStatus string

// const (
// 	taskStatusRunning   taskStatus = "RUNNING"
// 	taskStatusStarting  taskStatus = "STARTING"
// 	taskStatusError     taskStatus = "ERROR"
// 	taskStatusSucceeded taskStatus = "SUCCEEDED"
// )

type taskStatusMessage struct {
	Name   string
	Status api.TaskStatus
}

type Agent struct {
	ApiClient    *api.ClientWithResponses
	DockerClient *client.Client
	Token        *oauth2.Token
	// tokenSource
	ClientID           string
	AuthHost           string
	ApiHost            string
	Name               string
	PoolLabels         []string
	ConfigFileOverride string
	Status             agentStatus
	CurrentTaskName    string
	CurrentTaskStatus  api.TaskStatus
}

type Task api.TaskPollOutput

// TODO
// set up volumes
// upload outputs

func Start(a Agent) {
	err := a.loadConfig()
	if err != nil {
		log.Fatal("error loading config", err)
	}

	// TODO: check apiHost is available

	err = a.initializeDockerClient()
	if err != nil {
		log.Fatal("error initializing Docker client", err)
	}
	defer a.DockerClient.Close()

	err = a.checkAuth()
	if err != nil {
		log.Fatal("error in authentication")
	}

	ctx := context.Background()

	// start api.Client
	var tokenSource oauth2.TokenSource
	tokenSource = oauth2.ReuseTokenSource(a.Token, tokenSource)
	oauthClient := oauth2.NewClient(ctx, tokenSource)
	a.Token, err = tokenSource.Token()
	if err != nil {
		log.Fatal(err)
	}
	a.ApiClient, err = api.NewClientWithResponses(a.ApiHost, api.WithHTTPClient(oauthClient))
	if err != nil {
		log.Fatal(err)
	}
	defer a.saveCredentialCache()

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
		a.checkAuth()

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
			os.Exit(0)
		}
	}
}

func (a *Agent) initializeDockerClient() error {
	var err error
	a.DockerClient, err = client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return err
	}

	return nil
}

func (a Agent) pullImage(ctx context.Context, targetImage string) error {
	slog.Info("Pulling image", "image", targetImage)
	r, err := a.DockerClient.ImagePull(ctx, targetImage, image.PullOptions{
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

func GetConfigDir() (string, error) {
	expectedDir := os.ExpandEnv(ConfigPath)
	// Check first if the directory exists, and if it does not, create it:
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		err := os.Mkdir(expectedDir, 0700)
		if err != nil {
			log.Println("error creating directory:", err)
			return "", err
		}
	}
	return expectedDir, nil
}

func (a Agent) getTask() api.TaskPollOutput {
	ctx := context.Background()

	pollResponse, err := a.ApiClient.TaskPollWithResponse(ctx, api.TaskPollInput{
		AgentID:    a.Name,
		PoolLabels: a.PoolLabels,
	})
	if err != nil {
		slog.Error("error polling for task", "err", err)
	}

	// handle a 400 gracefully here

	if pollResponse.StatusCode() == 204 {
		slog.Debug("No task available")
		return api.TaskPollOutput{}
	}

	task := pollResponse.JSON200

	return *task
}

func stringifyEnvironmentVariables(inputVars [][]string) []string {
	var envVars []string
	for _, v := range inputVars {
		envVarString := fmt.Sprintf("%v=%v", v[0], v[1])
		envVars = append(envVars, envVarString)
	}
	return envVars
}

func (a Agent) runWorker(ctx context.Context, task Task, taskStateChan chan taskStatusMessage) error {
	providedEnvVars := stringifyEnvironmentVariables(*task.WorkerEnvironmentVariables)
	extraEnvVars := []string{
		"RERUN_WORKER_SHARED_MEMORY_MB=64",
		"RERUN_WORKER_ENVIRONMENT=dev",
		"RERUN_WORKER_GPU_COUNT=0",
		// TODO get the above from API
		"RERUN_WORKER_S3_ROLE_ARN=foo",
		// The above should be optional once the worker changes are in
	}

	// tmpDir, err := os.MkdirTemp("", fmt.Sprintf("resim-%v-*", *task.TaskName))
	// if err != nil {
	// 	slog.Error("Error creating tmp file", "err", err)
	// }

	config := &container.Config{
		Image: *task.WorkerImageURI,
		Env:   append(providedEnvVars, extraEnvVars...),
	}
	res, err := a.DockerClient.ContainerCreate(
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
					Type: mount.TypeBind,
					// Source: tmpDir,
					Source: "/tmp/resim",
					Target: "/tmp/resim",
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

	err = a.DockerClient.ContainerStart(ctx, res.ID, container.StartOptions{})
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
		status, err := a.DockerClient.ContainerInspect(ctx, res.ID)
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

			_, err := a.ApiClient.AgentHeartbeat(ctx, hbInput)
			if err != nil {
				log.Fatal(err)
			}
			// slog.Info("hb", "url", res.Request.URL, "status", res.StatusCode, "task_name", a.CurrentTaskName, "task_status", a.CurrentTaskStatus)
		}
	}()

	return nil
}

func (a *Agent) updateTaskStatus(ctx context.Context, taskName string, status api.TaskStatus) error {
	slog.Info("Updating task status", "task_name", taskName, "status", status)

	_, err := a.ApiClient.UpdateTask(ctx, taskName, api.UpdateTaskInput{
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
		err := os.Mkdir(dir, 0700)
		if err != nil {
			return err
		}
	}
	return nil
}
