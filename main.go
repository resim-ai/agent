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

	"github.com/davecgh/go-spew/spew"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
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
}

type Job struct {
	TaskName             string
	WorkerImageURI       string
	EnvironmentVariables []string
	Tags                 [][]string
}

type JobResponse struct {
	WorkerImageURI       string     `json:"workerImageURI"`
	EnvironmentVariables [][]string `json:"environmentVariables"`
	Tags                 [][]string `json:"tags"`
	TaskName             string     `json:"taskName"`
}

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

	ctx := context.Background()

	for {
		job := agent.getJob()
		agent.pullImage(ctx, job.WorkerImageURI)
		// agent.pullImage(ctx, job.CustomerImageURI)
		customerContainerID := agent.createCustomerContainer(job)
		fmt.Println(customerContainerID)
		time.Sleep(5 * time.Second)
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
	username := viper.GetString("username")
	password := viper.GetString("password")

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

	fmt.Println("body is", string(body))

	var jobResponse JobResponse
	err := json.Unmarshal(body, &jobResponse)
	if err != nil {
		log.Fatal(err)
	}

	spew.Dump(jobResponse)

	job := Job{
		EnvironmentVariables: stringifyEnvironmentVariables(jobResponse.EnvironmentVariables),
		WorkerImageURI:       jobResponse.WorkerImageURI,
		Tags:                 jobResponse.Tags,
		TaskName:             jobResponse.TaskName,
	}

	spew.Dump(job)
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
		&container.HostConfig{},
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
		envVars = append(envVars, envVarString)
	}
	return envVars
}
