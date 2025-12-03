package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/golang-jwt/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

const defaultTestConfig = `api-host: https://agentapi.resim.ai/agent/v1
auth-host: https://localhost
name: %s
pool-labels:
  - small
  - big
username: gimli
password: hunter2
aws-config-destination-dir: /container/aws
aws-config-source-dir: /foo/aws
mounts:
  - /lain/pain:/gain/iain
  - /len/landy:/lharon/lichael
environment-variables:
  - REPUNS_ENABLED=true
  - CAFFEINE_LEVEL=zero`

const (
	DefaultTestOrgName   = "test-org"
	DefaultTestAgentName = "this-is-my-agent-name"
)

type AgentTestSuite struct {
	suite.Suite
	agent          *Agent
	mockDocker     *MockDockerClient
	mockAuthServer *httptest.Server
	mockAPIServer  *httptest.Server
	configDir      string
}

func TestAgentSuite(s *testing.T) {
	suite.Run(s, new(AgentTestSuite))
}

func (s *AgentTestSuite) SetupTest() {
	s.mockDocker = &MockDockerClient{}
	s.agent = New(s.mockDocker)
	var err error
	s.agent.WorkerDir, err = os.MkdirTemp("", "test-worker-dir-*")
	if err != nil {
		s.FailNow("error creating worker dir", err)
	}
	s.agent.Name = DefaultTestAgentName
	s.agent.ContainerWatchInterval = 1 * time.Millisecond
	s.agent.AgentErrorSleep = 1 * time.Millisecond
	s.agent.WorkerExitSleep = 1 * time.Millisecond
	// also set via env vars in case LoadConfig() is called
	os.Setenv("RESIM_AGENT_AGENT_ERROR_SLEEP", "1ms")
	os.Setenv("RESIM_AGENT_WORKER_EXIT_SLEEP", "1ms")
	s.setupMockAuthServer()
	s.setupMockAPIServer()
	os.Setenv("RESIM_AGENT_ONE_TASK", "true")
}

func (s *AgentTestSuite) TearDownTest() {
	s.mockDocker.AssertExpectations(s.T())
	if s.mockAuthServer != nil {
		s.mockAuthServer.Close()
	}
	s.agent.AuthHost = ""
	os.Unsetenv("RESIM_AGENT_AUTH_HOST")
	if s.mockAPIServer != nil {
		s.mockAPIServer.Close()
	}
	s.agent.APIHost = ""
	os.Unsetenv("RESIM_AGENT_API_HOST")
	if s.configDir != "" {
		os.Remove(s.configDir)
	}
	os.Unsetenv("RESIM_AGENT_ONE_TASK")
	os.Unsetenv("RESIM_AGENT_AGENT_ERROR_SLEEP")
	os.Unsetenv("RESIM_AGENT_WORKER_EXIT_SLEEP")
	if s.agent.WorkerDir != "" {
		os.RemoveAll(s.agent.WorkerDir)
	}
}

func (s *AgentTestSuite) createConfigFile() string {
	t, _ := os.MkdirTemp("/tmp", "")
	f, err := os.Create(filepath.Join(t, "config.yaml"))
	if err != nil {
		log.Fatal("error creating temp file")
	}

	f.WriteString(fmt.Sprintf(defaultTestConfig, s.agent.Name))

	s.configDir = t
	return t
}

func (s *AgentTestSuite) TestLoadConfigFile() {
	configDir := s.createConfigFile()

	a := Agent{
		ConfigDirOverride: configDir,
	}

	os.Unsetenv("RESIM_AGENT_API_HOST") // set by default for tests; unset to validate reading it from the config file
	err := a.LoadConfig()
	s.NoError(err)

	s.Equal("https://agentapi.resim.ai/agent/v1", a.APIHost)
	s.Equal(s.agent.Name, a.Name)
}

func (s *AgentTestSuite) TestInvalidConfig() {
	a := Agent{
		ConfigDirOverride: "/not/real/path/",
	}

	authTs := s.setupMockAuthServer()
	defer authTs.Close()

	a.AuthHost = authTs.URL

	err := a.LoadConfig()
	s.Error(err)
}

func (s *AgentTestSuite) TestStringifyEnvironmentVariables() {
	inputVars := [][]string{
		{"RERUN_WORKER_FOO", "bar"},
		{"RERUN_WORKER_BAR", "foo"},
	}

	outputVars := StringifyEnvironmentVariables(inputVars)

	s.ElementsMatch([]string{
		"RERUN_WORKER_FOO=bar",
		"RERUN_WORKER_BAR=foo",
	}, outputVars)
}

func (s *AgentTestSuite) TestPrivilegedModeHostMode() {
	s.agent.ConfigDirOverride = s.createConfigFile()

	os.Setenv("RESIM_AGENT_PRIVILEGED", "true")
	defer os.Unsetenv("RESIM_AGENT_PRIVILEGED")

	os.Setenv("RESIM_AGENT_DOCKER_NETWORK_MODE", "host")
	defer os.Unsetenv("RESIM_AGENT_DOCKER_NETWORK_MODE")

	err := s.agent.LoadConfig()
	s.NoError(err)

	ioR := io.NopCloser(strings.NewReader("thing"))
	s.mockDocker.On(
		"ImagePull",
		mock.Anything,
		"public.ecr.aws/resim/experience-worker:ef41d3b7a46a502fef074eb1fd0a1aff54f7a538",
		mock.Anything).
		Return(ioR, nil).Once()

	containerID := uuid.UUID.String(uuid.New())

	var workerPrivilegedEnvVar string
	var workerDockerNetworkModeEnvVar string
	s.mockDocker.On(
		"ContainerCreate",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.MatchedBy(func(workerID string) bool {
			return strings.HasPrefix(workerID, "worker-")
		}),
	).Run(func(args mock.Arguments) {
		containerConfig := args.Get(1).(*container.Config)
		for _, envVar := range containerConfig.Env {
			if strings.HasPrefix(envVar, "RERUN_WORKER_PRIVILEGED") {
				workerPrivilegedEnvVar = envVar
			}
			if strings.HasPrefix(envVar, "RERUN_WORKER_DOCKER_NETWORK_MODE") {
				workerDockerNetworkModeEnvVar = envVar
			}
		}
	}).Return(container.CreateResponse{
		ID: containerID,
	}, nil).Once()

	s.mockDocker.On(
		"ContainerStart",
		mock.Anything,
		containerID,
		container.StartOptions{},
	).Return(nil).Once()

	runningContainer := createTestContainer("running", true)
	s.mockDocker.On("ContainerInspect", mock.Anything, containerID).Return(runningContainer, nil).Once()

	succeededContainer := createTestContainer("succeeded", false)
	s.mockDocker.On("ContainerInspect", mock.Anything, containerID).Return(succeededContainer, nil).Once()

	s.mockDocker.On(
		"ContainerRemove",
		mock.Anything,
		containerID,
		mock.Anything,
	).Return(nil).Once()

	err = s.agent.Start()
	s.NoError(err)

	// check the agent is running in privileged mode
	s.Equal(true, s.agent.Privileged)
	// check the agent uses the default bridge network mode
	s.Equal(DockerNetworkModeHost, s.agent.DockerNetworkMode)
	// check privileged mode is being passed through to the worker
	s.Equal("RERUN_WORKER_PRIVILEGED=true", workerPrivilegedEnvVar)
	// check docker network mode is being passed through to the worker
	s.Equal("RERUN_WORKER_DOCKER_NETWORK_MODE=host", workerDockerNetworkModeEnvVar)
}

// TODO(iain): DRY this out once we have more optionality
func (s *AgentTestSuite) TestDefaultAgentDockerModes() {
	s.agent.ConfigDirOverride = s.createConfigFile()

	err := s.agent.LoadConfig()
	s.NoError(err)

	ioR := io.NopCloser(strings.NewReader("thing"))
	s.mockDocker.On(
		"ImagePull",
		mock.Anything,
		"public.ecr.aws/resim/experience-worker:ef41d3b7a46a502fef074eb1fd0a1aff54f7a538",
		mock.Anything).
		Return(ioR, nil).Once()

	containerID := uuid.UUID.String(uuid.New())

	var workerPrivilegedEnvVar string
	var workerDockerNetworkModeEnvVar string
	var customConfigVar string
	var workerID string
	s.mockDocker.On(
		"ContainerCreate",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.MatchedBy(func(workerID string) bool {
			return strings.HasPrefix(workerID, "worker-")
		}),
	).Run(func(args mock.Arguments) {
		containerConfig := args.Get(1).(*container.Config)
		for _, envVar := range containerConfig.Env {
			if strings.HasPrefix(envVar, "RERUN_WORKER_PRIVILEGED") {
				workerPrivilegedEnvVar = envVar
			}
			if strings.HasPrefix(envVar, "RERUN_WORKER_DOCKER_NETWORK_MODE") {
				workerDockerNetworkModeEnvVar = envVar
			}
			if strings.HasPrefix(envVar, "RERUN_WORKER_CUSTOM_WORKER_CONFIG") {
				customConfigVar = envVar
			}
			if strings.HasPrefix(envVar, "RERUN_WORKER_WORKER_ID") {
				workerID = envVar
			}
		}
	}).Return(container.CreateResponse{
		ID: containerID,
	}, nil).Once()

	s.mockDocker.On(
		"ContainerStart",
		mock.Anything,
		containerID,
		container.StartOptions{},
	).Return(nil).Once()

	runningContainer := createTestContainer("running", true)
	s.mockDocker.On("ContainerInspect", mock.Anything, containerID).Return(runningContainer, nil).Once()

	succeededContainer := createTestContainer("succeeded", false)
	s.mockDocker.On("ContainerInspect", mock.Anything, containerID).Return(succeededContainer, nil).Once()

	s.mockDocker.On(
		"ContainerRemove",
		mock.Anything,
		containerID,
		mock.Anything,
	).Return(nil).Once()

	err = s.agent.Start()
	s.NoError(err)

	s.DirExists(s.agent.WorkerDir)
	// check the agent is running in privileged mode
	s.Equal(false, s.agent.Privileged)
	// check the agent uses the default bridge network mode
	s.Equal(DockerNetworkModeBridge, s.agent.DockerNetworkMode)
	// check privileged mode is not set
	s.Empty(workerPrivilegedEnvVar)
	// check docker network mode is being passed through to the worker
	s.Equal("RERUN_WORKER_DOCKER_NETWORK_MODE=bridge", workerDockerNetworkModeEnvVar)
	// check we can unmarshal the custom config
	var customConfig CustomWorkerConfig
	jsonPart := strings.TrimPrefix(customConfigVar, "RERUN_WORKER_CUSTOM_WORKER_CONFIG=")
	err = json.Unmarshal([]byte(jsonPart), &customConfig)
	s.NoError(err)
	expectedCustomConfig := CustomWorkerConfig{
		Mounts: []Mount{
			{Source: "/lain/pain", Target: "/gain/iain"},
			{Source: "/len/landy", Target: "/lharon/lichael"},
			{Source: "/foo/aws", Target: "/container/aws"},
		},
		EnvVars: []EnvVar{
			{Key: "REPUNS_ENABLED", Value: "true"},
			{Key: "CAFFEINE_LEVEL", Value: "zero"},
		},
		CacheDir: "/tmp/resim/cache",
	}
	s.Equal(expectedCustomConfig, customConfig)
	// validate the components of the workerID from the env var
	s.NotEmpty(workerID)
	workerID = strings.TrimPrefix(workerID, "RERUN_WORKER_WORKER_ID=")
	parts := strings.Split(workerID, "|")
	s.Equal(3, len(parts))
	s.Equal(fmt.Sprintf("agent-%s", DefaultTestOrgName), parts[0])
	s.Equal(DefaultTestAgentName, parts[1])
	s.NotEmpty(parts[2])
}

func (s *AgentTestSuite) TestStart_InitializeLoggingError() {
	s.agent.ConfigDirOverride = s.createConfigFile()

	err := s.agent.LoadConfig()
	s.NoError(err)

	// Test InitializeLogging error by setting an invalid log directory
	s.agent.LogDirOverride = "/invalid/path/that/cannot/be/created"

	// This should fail during InitializeLogging call
	err = s.agent.Start()
	s.ErrorContains(err, "can't make directories for new logfile")
}

func (s *AgentTestSuite) TestStart_CheckinError() {
	s.agent.ConfigDirOverride = s.createConfigFile()

	err := s.agent.LoadConfig()
	s.NoError(err)

	s.agent.APIHost = "invalid://host"

	err = s.agent.Start()
	s.ErrorContains(err, "error checking in")
}

func (s *AgentTestSuite) TestStart_MissingWorkerImageURI() {
	s.agent.ConfigDirOverride = s.createConfigFile()

	err := s.agent.LoadConfig()
	s.NoError(err)

	s.mockAPIServer.Close()
	s.mockAPIServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"authToken": "foo-worker-token", "workerEnvironmentVariables": [["RERUN_WORKER_STUFF", "yes"]]}`)
	}))
	s.agent.APIHost = s.mockAPIServer.URL

	err = s.agent.Start()
	s.ErrorContains(err, "no worker image URI (attempt 3)")
}

func (s *AgentTestSuite) TestStart_MissingWorkerEnvironmentVariables() {
	s.agent.ConfigDirOverride = s.createConfigFile()

	err := s.agent.LoadConfig()
	s.NoError(err)

	s.mockAPIServer.Close()
	s.mockAPIServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"workerImageURI": "public.ecr.aws/resim/experience-worker:ef41d3b7a46a502fef074eb1fd0a1aff54f7a538", "authToken": "foo-worker-token"}`)
	}))
	s.agent.APIHost = s.mockAPIServer.URL

	err = s.agent.Start()
	s.ErrorContains(err, "no worker environment variables (attempt 3)")
}

func (s *AgentTestSuite) TestStart_MissingAuthToken() {

	s.agent.ConfigDirOverride = s.createConfigFile()

	err := s.agent.LoadConfig()
	s.NoError(err)

	s.mockAPIServer.Close()
	s.mockAPIServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"workerImageURI": "public.ecr.aws/resim/experience-worker:ef41d3b7a46a502fef074eb1fd0a1aff54f7a538", "workerEnvironmentVariables": [["RERUN_WORKER_STUFF", "yes"]]}`)
	}))
	s.agent.APIHost = s.mockAPIServer.URL

	err = s.agent.Start()
	s.ErrorContains(err, "no auth token (attempt 3)")
}

func (s *AgentTestSuite) TestStart_MaybePullImageError() {
	s.agent.ConfigDirOverride = s.createConfigFile()

	err := s.agent.LoadConfig()
	s.NoError(err)

	s.mockDocker.On("ImagePull", mock.Anything, mock.Anything, mock.Anything).Return(io.NopCloser(strings.NewReader("thing")), errors.New("pull error"))

	err = s.agent.Start()
	s.ErrorContains(err, "pull error")
}

func (s *AgentTestSuite) TestStart_RunWorkerError() {
	s.agent.ConfigDirOverride = s.createConfigFile()

	err := s.agent.LoadConfig()
	s.NoError(err)

	s.mockDocker.On("ImagePull", mock.Anything, mock.Anything, mock.Anything).Return(io.NopCloser(strings.NewReader("thing")), nil)

	s.mockDocker.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(container.CreateResponse{
		ID: "container-id",
	}, errors.New("containercreate error"))

	s.mockDocker.On("ContainerRemove", mock.Anything, "container-id", mock.Anything).Return(nil)

	err = s.agent.Start()
	s.ErrorContains(err, "error running ReSim worker (attempt 3)")
	s.ErrorContains(err, "containercreate error")
	// The directory exists but should be empty
	files, err := os.ReadDir(s.agent.WorkerDir)
	s.NoError(err)
	s.Empty(files)
}

func (s *AgentTestSuite) TestStart_RunWorkerError_NoCleanup() {
	s.agent.ConfigDirOverride = s.createConfigFile()

	err := s.agent.LoadConfig()
	s.NoError(err)

	s.agent.RemoveWorkerDir = false

	s.mockDocker.On("ImagePull", mock.Anything, mock.Anything, mock.Anything).Return(io.NopCloser(strings.NewReader("thing")), nil)

	s.mockDocker.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(container.CreateResponse{
		ID: "container-id",
	}, errors.New("containercreate error"))

	s.mockDocker.On("ContainerRemove", mock.Anything, "container-id", mock.Anything).Return(nil)

	err = s.agent.Start()
	s.ErrorContains(err, "error running ReSim worker (attempt 3)")
	s.ErrorContains(err, "containercreate error")
	s.DirExists(s.agent.WorkerDir)
}

func (s *AgentTestSuite) TestGetOrgName() {
	s.agent.ConfigDirOverride = s.createConfigFile()

	err := s.agent.LoadConfig()
	s.NoError(err)

	err = s.agent.getOrgName()
	s.NoError(err)
	s.Equal(DefaultTestOrgName, s.agent.OrgName)
}

func (s *AgentTestSuite) TestGetOrgNameTokenError() {
	s.agent.ConfigDirOverride = s.createConfigFile()

	err := s.agent.LoadConfig()
	s.NoError(err)

	s.mockAuthServer.Close()
	s.mockAuthServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token": "ACCESS_TOKEN", "refresh_token": "REFRESH_TOKEN", "token_type": "bearer", "expires_in": 360000}`)
	}))
	s.agent.AuthHost = s.mockAuthServer.URL
	err = s.agent.getOrgName()
	s.ErrorContains(err, "invalid JWT")
	s.Empty(s.agent.OrgName)
}

func (s *AgentTestSuite) TestGetOrgNameNoOrgName() {
	s.agent.ConfigDirOverride = s.createConfigFile()

	err := s.agent.LoadConfig()
	s.NoError(err)

	s.mockAuthServer.Close()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"some-other-claim": "test-org",
	})
	tokenString, err := token.SignedString([]byte("secret"))
	s.NoError(err)

	s.mockAuthServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, fmt.Sprintf(`{"access_token": "%s", "refresh_token": "REFRESH_TOKEN", "token_type": "bearer", "expires_in": 360000}`, tokenString))
	}))
	s.agent.AuthHost = s.mockAuthServer.URL
	err = s.agent.getOrgName()
	s.ErrorContains(err, "no org claim in token")
	s.Empty(s.agent.OrgName)
}

func (s *AgentTestSuite) setupMockAuthServer() *httptest.Server {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		OrgIDClaim: DefaultTestOrgName,
	})
	tokenString, err := token.SignedString([]byte("secret"))
	s.NoError(err)

	s.mockAuthServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path != "/oauth/token" {
			s.FailNow("auth requested incorrect url")
		} else {
			io.WriteString(w, fmt.Sprintf(`{"access_token": "%s", "refresh_token": "REFRESH_TOKEN", "token_type": "bearer", "expires_in": 360000}`, tokenString))
		}
		if got, want := r.FormValue("grant_type"), "http://auth0.com/oauth/grant-type/password-realm"; got != want {
			s.FailNow("grant_type didn't match")
		}

	}))
	os.Setenv("RESIM_AGENT_AUTH_HOST", s.mockAuthServer.URL)
	s.agent.AuthHost = s.mockAuthServer.URL
	return s.mockAuthServer
}

func (s *AgentTestSuite) setupMockAPIServer() *httptest.Server {
	s.mockAPIServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch path := r.URL.Path; {
		case strings.HasSuffix(path, "/update"):
			io.WriteString(w, "")
		case path == "/heartbeat":
			io.WriteString(w, "")
		case strings.HasSuffix(path, "/checkin"):
			io.WriteString(w, `{"workerImageURI": "public.ecr.aws/resim/experience-worker:ef41d3b7a46a502fef074eb1fd0a1aff54f7a538", "authToken": "foo-worker-token", "workerEnvironmentVariables": [["RERUN_WORKER_STUFF", "yes"]]}`)
		default:
			s.FailNow(fmt.Sprintf("unknown path %v", r.URL.Path))
		}
	}))
	os.Setenv("RESIM_AGENT_API_HOST", s.mockAPIServer.URL)
	s.agent.APIHost = s.mockAPIServer.URL
	return s.mockAPIServer
}

func createTestContainer(status string, running bool) types.ContainerJSON {
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			State: &types.ContainerState{
				Status:  status,
				Running: running,
			},
		},
	}
}
