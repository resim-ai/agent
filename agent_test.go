package agent

import (
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"
)

type AgentTestSuite struct {
	suite.Suite
}

func TestAgentSuite(s *testing.T) {
	suite.Run(s, new(AgentTestSuite))
}

func (s *AgentTestSuite) TestStringifyEnvironmentVariables() {
	inputVars := [][]string{
		{"RERUN_WORKER_FOO", "bar"},
		{"RERUN_WORKER_BAR", "foo"},
	}

	outputVars := stringifyEnvironmentVariables(inputVars)

	s.ElementsMatch([]string{
		"RERUN_WORKER_FOO=bar",
		"RERUN_WORKER_BAR=foo",
	}, outputVars)
}

func (s *AgentTestSuite) TestLoadConfigFile() {
	f, err := os.CreateTemp("/tmp", "config.yaml")
	if err != nil {
		log.Fatal("error creating temp file")
	}
	defer os.Remove(f.Name())

	testConfig := `api-host: https://api.resim.ai/worker/v1
name: my-forklift
pool-labels: 
  - small
  - big
username: gimli
password: hunter2`

	f.WriteString(testConfig)

	a := Agent{
		ConfigFileOverride: f.Name(),
	}
	a.loadConfig()

	s.Equal("https://api.resim.ai/worker/v1", a.ApiHost)
	s.Equal("my-forklift", a.Name)
}
