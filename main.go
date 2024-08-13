package main

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"golang.org/x/oauth2"

	"github.com/spf13/viper"
)

const (
	EnvPrefix   = "RERUN_AGENT"
	LogLevelKey = "log-level"
	devClientID = "xJv0jqeP7QdPOsUidorgDlj4Mi74gVEW"
	audience    = "https://api.resim.ai"
)

const ConfigPath = "$HOME/resim"
const CredentialCacheFilename = "cache.json"

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
}

func main() {
	agent := Agent{}

	agent.initialize()
	defer agent.DockerClient.Close()

	agent.getWorkerImage()
}

func (a *Agent) initialize() {
	viper.SetEnvPrefix(EnvPrefix)
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	viper.SetDefault(LogLevelKey, "0") // info, default
	// TODO: work out how to convert strings into level numbers
	slog.SetLogLoggerLevel(slog.LevelDebug)

	var err error
	a.DockerClient, err = client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}

	var cache CredentialCache
	err = cache.loadCredentialCache()
	if err != nil {
		log.Println("Initializing credential cache")
	}
	defer cache.SaveCredentialCache()

	a.Token = a.authenticate(&cache)
}

func (a Agent) getWorkerImage() {
	ctx := context.Background()

	targetImage := "docker.io/library/nginx:latest"

	err := a.pullImage(ctx, targetImage)
	if err != nil {
		log.Fatal(err)
	}
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

func (c *CredentialCache) loadCredentialCache() error {
	homedir, _ := os.UserHomeDir()
	path := strings.ReplaceAll(filepath.Join(ConfigPath, CredentialCacheFilename), "$HOME", homedir)
	data, err := os.ReadFile(path)
	if err != nil {
		c.Tokens = map[string]oauth2.Token{}
		return err
	}

	return json.Unmarshal(data, &c.Tokens)
}

func (c *CredentialCache) SaveCredentialCache() {
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
