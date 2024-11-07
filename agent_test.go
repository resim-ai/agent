package main

import (
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

const defaultTestConfig = `api-host: https://api.resim.ai/worker/v1
name: my-forklift
pool-labels:
  - small
  - big
username: gimli
password: hunter2`

type AgentTestSuite struct {
	suite.Suite
}

func TestAgentSuite(s *testing.T) {
	suite.Run(s, new(AgentTestSuite))
}

func createConfigFile() string {
	t, _ := os.MkdirTemp("/tmp", "")
	f, err := os.Create(filepath.Join(t, "config.yaml"))
	if err != nil {
		log.Fatal("error creating temp file")
	}

	f.WriteString(defaultTestConfig)

	return f.Name()
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
	configFile := createConfigFile()
	defer os.Remove(configFile)

	a := Agent{
		ConfigDirOverride: filepath.Dir(configFile),
	}

	err := a.LoadConfig()
	s.NoError(err)

	s.Equal("https://api.resim.ai/worker/v1", a.APIHost)
	s.Equal("my-forklift", a.Name)
}

func (s *AgentTestSuite) TestInvalidConfig() {
	a := Agent{
		ConfigDirOverride: "/not/real/path/",
	}

	err := a.Start()

	s.Error(err)
}

func (s *AgentTestSuite) TestInvalidDockerClient() {
	os.Setenv("DOCKER_HOST", "1.2.3.4:1234")

	a := Agent{}

	err := a.Start()
	s.Error(err)

	os.Unsetenv("DOCKER_HOST")
}

func (s *AgentTestSuite) TestPrivilegedMode() {
	configFile := createConfigFile()
	defer os.Remove(configFile)

	a := Agent{
		ConfigDirOverride: filepath.Dir(configFile),
	}

	os.Setenv("RESIM_AGENT_PRIVILEGED", "true")

	err := a.LoadConfig()
	s.NoError(err)

	s.Equal(a.Privileged, true)
}
