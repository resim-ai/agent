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

	"github.com/davecgh/go-spew/spew"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/resim-ai/agent/api"
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
	APIClient    *api.ClientWithResponses
	DockerClient *client.Client
	Token        *oauth2.Token
	// tokenSource
	ClientID           string
	AuthHost           string
	APIHost            string
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
	slogLevel := new(slog.LevelVar)
	slog.SetDefault(
		slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})),
	)

	err := a.loadConfig()
	if err != nil {
		log.Fatal("error loading config", err)
	}

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

	// 1. can we do password-realm auth in the oauth2 client? no
	// 2. is there a way to pass a token straight in to the api client without using the oauth2 client?
	// 3. how do we make sure we save the token on exit (and refresh)?

	// start api.Client
	var tokenSource oauth2.TokenSource
	tokenSource = oauth2.ReuseTokenSource(a.Token, tokenSource)
	oauthClient := oauth2.NewClient(ctx, tokenSource)
	a.Token, err = tokenSource.Token()
	if err != nil {
		log.Fatal(err)
	}
	a.APIClient, err = api.NewClientWithResponses(a.APIHost, api.WithHTTPClient(oauthClient))
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

	for {
		a.checkAuth()

		task := a.getTask()
		if task.TaskName == nil {
			slog.Info("No task found. Snoozing...")
			time.Sleep(20 * time.Second)
			continue
		}
		slog.Info("Task found, starting work", "task name", task.TaskName)
		taskStateChan <- taskStatusMessage{
			Name:   *task.TaskName,
			Status: api.STARTING,
		}
		agentStateChan <- agentStatusRunning
		a.pullImage(ctx, *task.WorkerImageURI)
		// a.pullImage(ctx, *task.WorkerEnvironmentVariables[][])

		// customerContainerID := a.createCustomerContainer(task)
		// err := a.runCustomerContainer(ctx, customerContainerID, task.Name, taskStateChan)
		// if err != nil {
		// 	log.Fatal(err)
		// }
		// agentStateChan <- agentStatusIdle
		// time.Sleep(50 * time.Second)
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
		err := os.Mkdir(expectedDir, 0o700)
		if err != nil {
			slog.Error("Error creating directory")
			return "", err
		}
	}
	return expectedDir, nil
}

func (a Agent) getTask() api.TaskPollOutput {
	ctx := context.Background()

	pollResponse, err := a.APIClient.TaskPollWithResponse(ctx, api.TaskPollInput{
		WorkerID:   a.Name,
		PoolLabels: a.PoolLabels,
	})
	if err != nil {
		slog.Error("Error polling for task", "err", err)
	}
	if pollResponse.StatusCode() == 204 {
		slog.Debug("No task available")
		return api.TaskPollOutput{}
	}

	task := pollResponse.JSON200
	fmt.Println(task)

	return api.TaskPollOutput{}
}

func (a Agent) createCustomerContainer(task Task) string {
	config := &container.Config{
		// Image:      task.WorkerImageURI,
		Image: "public.ecr.aws/ubuntu/ubuntu:latest",
		Cmd:   []string{"sleep", "90"},
		// Env:   stringifyEnvironmentVariables(task.EnvironmentVariables),
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
					Type:   mount.TypeBind,
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

	return res.ID
}

func stringifyEnvironmentVariables(inputVars map[string]string) []string {
	var envVars []string
	for k, v := range inputVars {
		envVarString := fmt.Sprintf("%v=%v", k, v)
		envVars = append(envVars, envVarString)
	}
	return envVars
}

func (a Agent) runCustomerContainer(ctx context.Context, containerID string, taskName string, taskStateChan chan taskStatusMessage) error {
	err := a.DockerClient.ContainerStart(ctx, containerID, container.StartOptions{})
	if err != nil {
		return err
	}
	slog.Info("container started")
	taskStateChan <- taskStatusMessage{
		Name:   taskName,
		Status: api.RUNNING,
	}
	for {
		status, err := a.DockerClient.ContainerInspect(ctx, containerID)
		if err != nil {
			return err
		}
		if status.State.Status != "running" {
			slog.Info("container is not running")
			// TODO handle error or succeeded by checking exit code
			taskStateChan <- taskStatusMessage{
				Name:   taskName,
				Status: api.SUCCEEDED,
			}
			break
		} else {
			slog.Info("container is running")
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func (a *Agent) startHeartbeat(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)

	// url := fmt.Sprintf("%v/agent/heartbeat", a.ApiHost)
	// none := "none"
	hbInput := api.AgentHeartbeatInput{
		AgentName:  &a.Name,
		PoolLabels: &a.PoolLabels,
		TaskName:   nil,
	}

	go func() {
		for range ticker.C {

			if a.CurrentTaskName != "" {
				hbInput.TaskName = &a.CurrentTaskName
			}
			if a.CurrentTaskStatus != "" {
				hbInput.TaskStatus = &a.CurrentTaskStatus
			}

			res, err := a.APIClient.AgentHeartbeat(ctx, hbInput)
			if err != nil {
				log.Fatal(err)
			}
			slog.Info("hb", "url", res.Request.URL, "status", res.StatusCode, "body", res.Request.Body)
		}
	}()

	return nil
}

func (a *Agent) updateTaskStatus(ctx context.Context, taskName string, status api.TaskStatus) error {
	a.CurrentTaskName = taskName
	a.CurrentTaskStatus = status

	res, err := a.APIClient.UpdateTask(ctx, taskName, api.UpdateTaskInput{
		Status: &status,
	})
	spew.Dump(res.StatusCode)

	return err
}
