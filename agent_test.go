package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

const defaultTestConfig = `api-host: https://agentapi.resim.ai/agent/v1
auth-host: https://localhost
name: my-forklift
pool-labels:
  - small
  - big
username: gimli
password: hunter2`

type AgentTestSuite struct {
	suite.Suite
	agent      *Agent
	mockDocker *MockDockerClient
}

func TestAgentSuite(s *testing.T) {
	suite.Run(s, new(AgentTestSuite))
}

func (s *AgentTestSuite) SetupTest() {
	s.mockDocker = &MockDockerClient{}
	s.agent = New(s.mockDocker)
}

func (s *AgentTestSuite) TearDownTest() {
	s.mockDocker.AssertExpectations(s.T())
}

func createConfigFile() string {
	t, _ := os.MkdirTemp("/tmp", "")
	f, err := os.Create(filepath.Join(t, "config.yaml"))
	if err != nil {
		log.Fatal("error creating temp file")
	}

	f.WriteString(defaultTestConfig)

	return t
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

func (s *AgentTestSuite) TestLoadConfigFile() {
	configDir := createConfigFile()
	defer os.Remove(configDir)

	a := Agent{
		ConfigDirOverride: configDir,
	}

	err := a.LoadConfig()
	s.NoError(err)

	s.Equal("https://agentapi.resim.ai/agent/v1", a.APIHost)
	s.Equal("my-forklift", a.Name)
}

func (s *AgentTestSuite) TestInvalidConfig() {
	a := Agent{
		ConfigDirOverride: "/not/real/path/",
	}

	err := a.Start()

	s.Error(err)
}

func (s *AgentTestSuite) TestPrivilegedMode() {
	configDir := createConfigFile()
	defer os.Remove(configDir)

	s.agent.ConfigDirOverride = configDir

	authTs := s.mockAuthServer()
	defer authTs.Close()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch path := r.URL.Path; {
		case path == "/task/poll":
			io.WriteString(w, dummyTaskResponse)
		case strings.HasSuffix(path, "/update"):
			io.WriteString(w, "")
		case path == "/heartbeat":
			io.WriteString(w, "")
		default:
			s.FailNow(fmt.Sprintf("unknown path %v", r.URL.Path))
		}
	}))
	defer ts.Close()

	os.Setenv("RESIM_AGENT_PRIVILEGED", "true")
	defer os.Unsetenv("RESIM_AGENT_PRIVILEGED")

	os.Setenv("RESIM_AGENT_ONE_TASK", "true")
	defer os.Unsetenv("RESIM_AGENT_ONE_TASK")

	os.Setenv("RESIM_AGENT_AUTH_HOST", authTs.URL)
	defer os.Unsetenv("RESIM_AGENT_AUTH_HOST")

	os.Setenv("RESIM_AGENT_API_HOST", ts.URL)
	defer os.Unsetenv("RESIM_AGENT_API_HOST")

	var taskInput Task
	json.Unmarshal([]byte(dummyTaskResponse), &taskInput)

	ioR := io.NopCloser(strings.NewReader("thing"))
	s.mockDocker.On(
		"ImagePull",
		mock.Anything,
		"public.ecr.aws/resim/experience-worker:ef41d3b7a46a502fef074eb1fd0a1aff54f7a538",
		mock.Anything).
		Return(ioR, nil).Once()

	containerID := uuid.UUID.String(uuid.New())

	var workerPrivilegedEnvVar string
	s.mockDocker.On(
		"ContainerCreate",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		*taskInput.TaskName,
	).Run(func(args mock.Arguments) {
		containerConfig := args.Get(1).(*container.Config)
		for _, envVar := range containerConfig.Env {
			if strings.HasPrefix(envVar, "RERUN_WORKER_PRIVILEGED") {
				workerPrivilegedEnvVar = envVar
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

	inspectionResult1 := new(types.ContainerJSON)
	inspectionResult1.ContainerJSONBase = new(types.ContainerJSONBase)
	inspectionResult1.State = new(types.ContainerState)
	inspectionResult1.State.Status = "running"
	inspectionResult1.State.Running = true

	s.mockDocker.On(
		"ContainerInspect",
		mock.Anything,
		containerID,
	).Return(*inspectionResult1, nil).Once()

	inspectionResult2 := new(types.ContainerJSON)
	inspectionResult2.ContainerJSONBase = new(types.ContainerJSONBase)
	inspectionResult2.State = new(types.ContainerState)
	inspectionResult2.State.Status = "succeeded"
	inspectionResult2.State.Running = false

	s.mockDocker.On(
		"ContainerInspect",
		mock.Anything,
		containerID,
	).Return(*inspectionResult2, nil).Once()

	err := s.agent.Start()
	s.NoError(err)

	// check the agent is running in privileged mode
	s.Equal(s.agent.Privileged, true)
	// check privileged mode is being passed through to the worker
	s.Equal(workerPrivilegedEnvVar, "RERUN_WORKER_PRIVILEGED=true")
}

func (s *AgentTestSuite) mockAuthServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path != "/oauth/token" {
			s.FailNow("auth requested incorrect url")
		} else {
			io.WriteString(w, `{"access_token": "ACCESS_TOKEN", "refresh_token": "REFRESH_TOKEN", "token_type": "bearer", "expires_in": 360000}`)
		}
		if got, want := r.FormValue("grant_type"), "http://auth0.com/oauth/grant-type/password-realm"; got != want {
			s.FailNow("grant_type didn't match")
		}

	}))
}
