package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
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
	ApiClient          *api.Client
	DockerClient       *client.Client
	Token              *oauth2.Token
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

type Task struct {
	Name                 string
	WorkerImageURI       string
	CustomerImageURI     string
	EnvironmentVariables map[string]string
	Tags                 [][]string
}

type TaskResponse struct {
	Name                 string     `json:"taskName"`
	WorkerImageURI       string     `json:"workerImageURI"`
	EnvironmentVariables [][]string `json:"environmentVariables"`
	Tags                 [][]string `json:"tags"`
}

// TODO
// set up volumes
// upload outputs

func Start(a Agent) {
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

	// start api.Client
	var tokenSource oauth2.TokenSource
	tokenSource = oauth2.ReuseTokenSource(a.Token, tokenSource)
	oauthClient := oauth2.NewClient(ctx, tokenSource)
	a.Token, err = tokenSource.Token()
	if err != nil {
		log.Fatal(err)
	}
	a.ApiClient, err = api.NewClient(a.ApiHost, api.WithHTTPClient(oauthClient))
	if err != nil {
		log.Fatal(err)
	}

	spew.Dump(a.Token)
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
		taskStateChan <- taskStatusMessage{
			Name:   task.Name,
			Status: api.STARTING,
		}
		agentStateChan <- agentStatusRunning
		a.pullImage(ctx, task.WorkerImageURI)
		a.pullImage(ctx, task.CustomerImageURI)

		customerContainerID := a.createCustomerContainer(task)
		err := a.runCustomerContainer(ctx, customerContainerID, task.Name, taskStateChan)
		if err != nil {
			log.Fatal(err)
		}
		agentStateChan <- agentStatusIdle
		time.Sleep(50 * time.Second)
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
		err := os.Mkdir(expectedDir, 0700)
		if err != nil {
			log.Println("error creating directory:", err)
			return "", err
		}
	}
	return expectedDir, nil
}

func (a Agent) getTask() Task {
	url := fmt.Sprintf("%v/task/poll", a.ApiHost)
	jsonBody := []byte(`{"workerID": "big-yin", "poolLabels": ["small-hil"]}`)
	// jsonBody := []byte(`{"workerID": "big-yin", "poolLabels": ["small-hil", "big-hil"]}`) // example of no content
	bodyReader := bytes.NewReader(jsonBody)

	req, _ := http.NewRequest(http.MethodPost, url, bodyReader)

	req.Header.Add("authorization", fmt.Sprintf("Bearer %v", a.Token.AccessToken))
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var taskResponse TaskResponse
	err = json.Unmarshal(body, &taskResponse)
	if err != nil {
		log.Fatal(err)
	}

	environmentVariables := make(map[string]string)
	for _, v := range taskResponse.EnvironmentVariables {
		environmentVariables[v[0]] = v[1]
	}

	task := Task{
		EnvironmentVariables: environmentVariables,
		WorkerImageURI:       taskResponse.WorkerImageURI,
		Tags:                 taskResponse.Tags,
		Name:                 taskResponse.Name,
	}
	task.CustomerImageURI = environmentVariables["RERUN_WORKER_BUILD_IMAGE_URI"]

	return task
}

func (a Agent) createCustomerContainer(task Task) string {
	config := &container.Config{
		// Image:      task.WorkerImageURI,
		Image: "public.ecr.aws/ubuntu/ubuntu:latest",
		Cmd:   []string{"sleep", "90"},
		Env:   stringifyEnvironmentVariables(task.EnvironmentVariables),
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
		task.Name,
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

	hbInput := api.AgentHeartbeatInput{
		AgentName:  &a.Name,
		PoolLabels: &a.PoolLabels,
	}

	go func() {
		for range ticker.C {

			hbInput.TaskName = &a.CurrentTaskName
			hbInput.TaskStatus = &a.CurrentTaskStatus
			if hbInput.TaskName == nil {
				none := "none"
				hbInput.TaskName = &none
			}

			spew.Dump(hbInput)

			res, err := a.ApiClient.AgentHeartbeat(ctx, hbInput)
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

	res, err := a.ApiClient.UpdateTask(ctx, taskName, api.UpdateTaskInput{
		Status: &status,
	})
	spew.Dump(res.StatusCode)

	return err
}
