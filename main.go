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

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/oauth2"
)

type Agent struct {
	DockerClient       *client.Client
	Token              oauth2.Token
	ClientID           string
	AuthHost           string
	ApiHost            string
	Name               string
	PoolLabels         []string
	ConfigFileOverride string
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

func Start(agent Agent) {
	err := agent.loadConfig()
	if err != nil {
		log.Fatal("error loading config", err)
	}

	err = agent.initializeDockerClient()
	if err != nil {
		log.Fatal("error initializing Docker client", err)
	}
	defer agent.DockerClient.Close()

	err = agent.checkAuth()
	if err != nil {
		log.Fatal("error in authentication")
	}

	agent.startHeartbeat()

	ctx := context.Background()

	for {
		agent.checkAuth()

		task := agent.getTask()
		agent.pullImage(ctx, task.WorkerImageURI)
		agent.pullImage(ctx, task.CustomerImageURI)

		customerContainerID := agent.createCustomerContainer(task)
		err := agent.runCustomerContainer(ctx, customerContainerID)
		if err != nil {
			log.Fatal(err)
		}
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
		Image: task.WorkerImageURI,
		// Cmd:   []string{"echo", "hello world"},
		Env: stringifyEnvironmentVariables(task.EnvironmentVariables),
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

func (a Agent) runCustomerContainer(ctx context.Context, containerID string) error {
	err := a.DockerClient.ContainerStart(ctx, containerID, container.StartOptions{})
	if err != nil {
		return err
	}
	slog.Info("container started")
	for {
		status, err := a.DockerClient.ContainerInspect(ctx, containerID)
		if err != nil {
			return err
		}
		if status.State.Status != "running" {
			slog.Info("container is not running")
			break
		} else {
			slog.Info("container is running")
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func (a *Agent) startHeartbeat() error {
	ticker := time.NewTicker(30 * time.Second)

	url := fmt.Sprintf("%v/agent/heartbeat", a.ApiHost)

	go func() {
		for range ticker.C {
			a.checkAuth()

			jsonBody := []byte(`{"agentName": "%v", "poolLabels": ["small-hil"]}`)
			bodyReader := bytes.NewReader(jsonBody)

			hb, _ := http.NewRequest(http.MethodPost, url, bodyReader)
			hb.Header.Add("authorization", fmt.Sprintf("Bearer %v", a.Token.AccessToken))
			hb.Header.Set("Content-Type", "application/json")
			_, err := http.DefaultClient.Do(hb)
			if err != nil {
				slog.Error("error in heartbeat")
			}
		}
	}()

	return nil
}
