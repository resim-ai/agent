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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	// containerd "github.com/containerd/containerd/v2/client"
	// "github.com/containerd/containerd/v2/pkg/namespaces"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/oauth2"

	"github.com/spf13/viper"
)

const (
	devClientID = "xJv0jqeP7QdPOsUidorgDlj4Mi74gVEW"
	audience    = "https://api.resim.ai"
)

type tokenJSON struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int32  `json:"expires_in"`
}

type CredentialCache struct {
	Tokens      map[string]oauth2.Token `json:"tokens"`
	TokenSource oauth2.TokenSource
	ClientID    string
}

type Agent struct {
	DockerClient *client.Client
	Token        oauth2.Token
	AuthHost     string
	ApiHost      string
	Name         string
	PoolLabels   []string
}

type Job struct {
	TaskName             string
	WorkerImageURI       string
	CustomerImageURI     string
	EnvironmentVariables []string
	Tags                 [][]string
}

type JobResponse struct {
	WorkerImageURI       string     `json:"workerImageURI"`
	EnvironmentVariables [][]string `json:"environmentVariables"`
	Tags                 [][]string `json:"tags"`
	TaskName             string     `json:"taskName"`
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

	cache := loadCredentialCache()
	agent.Token = agent.authenticate(&cache)
	saveCredentialCache(&cache)

	agent.startHeartbeat()

	ctx := context.Background()

	for {
		job := agent.getJob()
		agent.pullImage(ctx, job.WorkerImageURI)
		agent.pullImage(ctx, job.CustomerImageURI)

		// loop through env vars and find RERUN_WORKER_BUILD_IMAGE_URI
		// agent.pullImage(ctx, job.CustomerImageURI)

		customerContainerID := agent.createCustomerContainer(job)
		err := agent.runCustomerContainer(ctx, customerContainerID)
		if err != nil {
			log.Fatal(err)
		}
		time.Sleep(5000 * time.Second)
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

func (a Agent) authenticate(cache *CredentialCache) oauth2.Token {
	var token oauth2.Token
	var tokenSource oauth2.TokenSource

	// TODO dev/prod logic
	clientID := devClientID
	tokenURL := "https://resim-dev.us.auth0.com/oauth/token"
	username := viper.GetString(UsernameKey)
	password := viper.GetString(PasswordKey)

	cache.ClientID = clientID

	token, ok := cache.Tokens[clientID]
	if !(ok && token.Valid()) {

		payloadVals := url.Values{
			"grant_type": []string{"http://auth0.com/oauth/grant-type/password-realm"},
			"realm":      []string{"agents"},
			"username":   []string{username},
			"password":   []string{password},
			"audience":   []string{audience},
			"client_id":  []string{clientID},
		}

		req, _ := http.NewRequest("POST", tokenURL, strings.NewReader(payloadVals.Encode()))

		req.Header.Add("content-type", "application/x-www-form-urlencoded")

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal("error in password auth: ", err)
		}

		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)

		var tj tokenJSON
		err = json.Unmarshal(body, &tj)
		if err != nil {
			log.Fatal(err)
		}
		token = oauth2.Token{
			AccessToken:  tj.AccessToken,
			TokenType:    tj.TokenType,
			RefreshToken: tj.RefreshToken,
			Expiry:       time.Now().Add(time.Duration(tj.ExpiresIn) * time.Second),
		}
	}

	cache.TokenSource = oauth2.ReuseTokenSource(&token, tokenSource)

	return token
}

func loadCredentialCache() CredentialCache {
	homedir, _ := os.UserHomeDir()
	path := strings.ReplaceAll(filepath.Join(ConfigPath, CredentialCacheFilename), "$HOME", homedir)
	var c CredentialCache
	c.Tokens = map[string]oauth2.Token{}
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &c.Tokens)
	}

	return c
}

func saveCredentialCache(c *CredentialCache) {
	token, err := c.TokenSource.Token()
	if err != nil {
		log.Println("error getting token:", err)
	}
	if token != nil {
		c.Tokens[c.ClientID] = *token
	}

	data, err := json.Marshal(c.Tokens)
	if err != nil {
		log.Println("error marshaling credential cache:", err)
		return
	}

	expectedDir, err := GetConfigDir()
	if err != nil {
		return
	}

	path := filepath.Join(expectedDir, CredentialCacheFilename)
	err = os.WriteFile(path, data, 0600)
	if err != nil {
		log.Println("error saving credential cache:", err)
	}
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

func (a Agent) getJob() Job {
	url := fmt.Sprintf("%v/task/poll", a.ApiHost)
	jsonBody := []byte(`{"workerID": "big-yin", "poolLabels": ["small-hil"]}`)
	// jsonBody := []byte(`{"workerID": "big-yin", "poolLabels": ["small-hil", "big-hil"]}`) // example of no content
	bodyReader := bytes.NewReader(jsonBody)

	req, _ := http.NewRequest(http.MethodPost, url, bodyReader)

	req.Header.Add("authorization", fmt.Sprintf("Bearer %v", a.Token.AccessToken))
	req.Header.Set("Content-Type", "application/json")

	res, _ := http.DefaultClient.Do(req)

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var jobResponse JobResponse
	err := json.Unmarshal(body, &jobResponse)
	if err != nil {
		log.Fatal(err)
	}

	job := Job{
		EnvironmentVariables: stringifyEnvironmentVariables(jobResponse.EnvironmentVariables),
		WorkerImageURI:       jobResponse.WorkerImageURI,
		Tags:                 jobResponse.Tags,
		TaskName:             jobResponse.TaskName,
	}
	job.CustomerImageURI = getCustomerImageURI(jobResponse.EnvironmentVariables)

	return job
}

func (a Agent) createCustomerContainer(job Job) string {
	config := &container.Config{
		Image: job.WorkerImageURI,
		// Cmd:   []string{"echo", "hello world"},
		Env: job.EnvironmentVariables,
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
		job.TaskName,
	)
	if err != nil {
		fmt.Println(err)
	}

	return res.ID
}

func stringifyEnvironmentVariables(inputVars [][]string) []string {
	var envVars []string
	for _, envVar := range inputVars {
		envVarString := strings.Join(envVar, "=")

		if envVar[0] == "RERUN_WORKER_AUTH_TOKEN" {
			envVarString = fmt.Sprintf("%v={\"access_token\":\"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6IlU5cm5vRXhFWV9PSi1pT1lncmg5TiJ9.eyJodHRwczovL2FwaS5yZXNpbS5haS9vcmdfaWQiOiJlMmUucmVzaW0uYWkiLCJodHRwczovL2FwaS5yZXNpbS5haS9lbWFpbCI6IndvcmtlcitlMmUucmVzaW0uYWlAcmVzaW0uYWkiLCJodHRwczovL2FwaS5yZXNpbS5haS93b3JrZXJfdHlwZSI6InVuaXZlcnNhbCIsImlzcyI6Imh0dHBzOi8vcmVzaW0tZGV2LnVzLmF1dGgwLmNvbS8iLCJzdWIiOiJjN2lOUGh0clJ0OTR5R3VkWVA1ZlZSbzh1M2RqTEVPOEBjbGllbnRzIiwiYXVkIjoiaHR0cHM6Ly9hcGkucmVzaW0uYWkiLCJpYXQiOjE3MjM3MzY2NTIsImV4cCI6MTcyMzgyMzA1Miwic2NvcGUiOiJ3b3JrZXIgZXhwZXJpZW5jZXM6cmVhZCBleHBlcmllbmNlczp3cml0ZSBleHBlcmllbmNlVGFnczpyZWFkIGV4cGVyaWVuY2VUYWdzOndyaXRlIHByb2plY3RzOnJlYWQgcHJvamVjdHM6d3JpdGUgYmF0Y2hlczpyZWFkIGJhdGNoZXM6d3JpdGUgYnVpbGRzOnJlYWQgYnVpbGRzOndyaXRlIHZpZXc6cmVhZCB2aWV3OndyaXRlIHN5c3RlbXM6cmVhZCBzeXN0ZW1zOndyaXRlIHN3ZWVwczpyZWFkIHN3ZWVwczp3cml0ZSByZXBvcnRzOnJlYWQgcmVwb3J0czp3cml0ZSIsImd0eSI6ImNsaWVudC1jcmVkZW50aWFscyIsImF6cCI6ImM3aU5QaHRyUnQ5NHlHdWRZUDVmVlJvOHUzZGpMRU84IiwicGVybWlzc2lvbnMiOlsid29ya2VyIiwiZXhwZXJpZW5jZXM6cmVhZCIsImV4cGVyaWVuY2VzOndyaXRlIiwiZXhwZXJpZW5jZVRhZ3M6cmVhZCIsImV4cGVyaWVuY2VUYWdzOndyaXRlIiwicHJvamVjdHM6cmVhZCIsInByb2plY3RzOndyaXRlIiwiYmF0Y2hlczpyZWFkIiwiYmF0Y2hlczp3cml0ZSIsImJ1aWxkczpyZWFkIiwiYnVpbGRzOndyaXRlIiwidmlldzpyZWFkIiwidmlldzp3cml0ZSIsInN5c3RlbXM6cmVhZCIsInN5c3RlbXM6d3JpdGUiLCJzd2VlcHM6cmVhZCIsInN3ZWVwczp3cml0ZSIsInJlcG9ydHM6cmVhZCIsInJlcG9ydHM6d3JpdGUiXX0.ZAydfqgKNnHbC0KbyQh1gHwQrtRM9v794QslK1pfwGNWAgdZuTKkpKaYFdEClSdU3R5hwo16GVMqXB4BRIcPc09AU2fcX0qk5oGGdFucIuSBG50GMnCBCGWtWx9Jnl7FCqHoncsJEjf-b-E2Y35anIJDcyjN0j2_3szQW7-dDp4v7Q9J8nSl2YdW-zxG3FscYkLeAzCwJf_ESHpCFtKCmsw1fo6V_SCJojiU4EfFf2tDCMiyBpsIFHI5vHe7i68GIicGDGa6KDTcdNJY2ky58blL4Ie0tEbKMqol8tgqoyCdG3Zb48pU9ZG_S-C5FJ8R-Vr_ThFnjQT6LoCbbUV1LA\",\"token_type\":\"Bearer\",\"expiry\":\"2024-08-16T15:44:12.41592629Z\"}", envVar[0])
		}
		if envVar[0] == "RERUN_WORKER_BUILD_IMAGE_URI" {
			envVarString = fmt.Sprintf("%v=public.ecr.aws/docker/library/hello-world:latest", envVar[0])
		}
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

func (a Agent) startHeartbeat() error {
	ticker := time.NewTicker(30 * time.Second)

	url := fmt.Sprintf("%v/agent/heartbeat", a.ApiHost)

	go func() {
		for range ticker.C {
			jsonBody := []byte(`{"agentName": "%v", "poolLabels": ["small-hil"]}`)
			bodyReader := bytes.NewReader(jsonBody)

			hb, _ := http.NewRequest(http.MethodPost, url, bodyReader)

			hb.Header.Add("authorization", fmt.Sprintf("Bearer %v", a.Token.AccessToken))
			hb.Header.Set("Content-Type", "application/json")
			res, _ := http.DefaultClient.Do(hb)
			fmt.Println(res.Status)
		}
	}()

	return nil
}

func getCustomerImageURI(envVars [][]string) string {
	for _, envVar := range envVars {
		if envVar[0] == "RERUN_WORKER_BUILD_IMAGE_URI" {
			return envVar[1]
		}
	}
	return ""
}
