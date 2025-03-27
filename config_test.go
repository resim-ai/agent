package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ConfigTestSuite struct {
	suite.Suite
	agent         *Agent
	tempHomeDir   string
	tempConfigDir string
}

func TestConfigSuite(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}

func (s *ConfigTestSuite) SetupTest() {
	s.agent = New(nil)

	tempDir, err := os.MkdirTemp("", "test-config-*")
	if err != nil {
		s.T().Fatalf("Failed to create temp dir: %v", err)
	}

	s.tempConfigDir = filepath.Join(tempDir, "resim")
	err = os.MkdirAll(s.tempConfigDir, 0700)
	if err != nil {
		s.T().Fatalf("Failed to create temp config dir: %v", err)
	}
	s.agent.ConfigDirOverride = s.tempConfigDir
}

func (s *ConfigTestSuite) TearDownTest() {
	// Clean up temporary directory
	os.RemoveAll(filepath.Dir(s.tempConfigDir))
}

func (s *ConfigTestSuite) createConfigFile(content string) {
	configFile := filepath.Join(s.tempConfigDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(content), 0600)
	if err != nil {
		s.T().Fatalf("Failed to write config file: %v", err)
	}
}

func (s *ConfigTestSuite) TestLoadConfigBasic() {
	// Basic configuration
	s.createConfigFile(`
api-host: https://test-api.resim.ai/agent/v1
auth-host: https://test.us.auth0.com
name: test-agent
pool-labels:
  - small
  - test
log-level: debug
`)

	err := s.agent.LoadConfig()
	s.NoError(err)

	s.Equal("https://test-api.resim.ai/agent/v1", s.agent.APIHost)
	s.Equal("https://test.us.auth0.com", s.agent.AuthHost)
	s.Equal("test-agent", s.agent.Name)
	s.Equal([]string{"small", "test"}, s.agent.PoolLabels)
	s.Equal("debug", s.agent.LogLevel)
	s.Equal(false, s.agent.AutoUpdate)                          // default value
	s.Equal(false, s.agent.Privileged)                          // default value
	s.Equal(DockerNetworkModeBridge, s.agent.DockerNetworkMode) // default value
	s.Empty(s.agent.CustomerWorkerConfig.Mounts)
	s.Empty(s.agent.CustomerWorkerConfig.EnvVars)
}

func (s *ConfigTestSuite) TestLoadConfigMounts() {
	// Configuration with mounts
	s.createConfigFile(`
api-host: https://test-api.resim.ai/agent/v1
auth-host: https://test.us.auth0.com
name: test-agent
pool-labels:
  - small
  - test
mounts:
  - /host/path1:/container/path1
  - /host/path2:/container/path2
`)

	err := s.agent.LoadConfig()
	s.NoError(err)

	s.Len(s.agent.CustomerWorkerConfig.Mounts, 2)
	s.Equal("/host/path1", s.agent.CustomerWorkerConfig.Mounts[0].Source)
	s.Equal("/container/path1", s.agent.CustomerWorkerConfig.Mounts[0].Target)
	s.Equal("/host/path2", s.agent.CustomerWorkerConfig.Mounts[1].Source)
	s.Equal("/container/path2", s.agent.CustomerWorkerConfig.Mounts[1].Target)
}

func (s *ConfigTestSuite) TestLoadConfigEnvironmentVariables() {
	// Configuration with environment variables
	s.createConfigFile(`
api-host: https://test-api.resim.ai/agent/v1
auth-host: https://test.us.auth0.com
name: test-agent
pool-labels:
  - small
environment-variables:
  - TEST_KEY1=test_value1
  - TEST_KEY2=test_value2
`)

	err := s.agent.LoadConfig()
	s.NoError(err)

	s.Len(s.agent.CustomerWorkerConfig.EnvVars, 2)
	s.Equal("TEST_KEY1", s.agent.CustomerWorkerConfig.EnvVars[0].Key)
	s.Equal("test_value1", s.agent.CustomerWorkerConfig.EnvVars[0].Value)
	s.Equal("TEST_KEY2", s.agent.CustomerWorkerConfig.EnvVars[1].Key)
	s.Equal("test_value2", s.agent.CustomerWorkerConfig.EnvVars[1].Value)
}

func (s *ConfigTestSuite) TestLoadConfigAWSDirectory() {
	// Create mock AWS directory in the temp home
	awsDir := filepath.Join(s.tempHomeDir, ".aws")
	err := os.MkdirAll(awsDir, 0700)
	s.NoError(err)

	// "Dependency Injection a la Golang"
	s.agent.getAWSConfigDirFunc = func() (string, bool) {
		return awsDir, true
	}

	s.createConfigFile(`
api-host: https://test-api.resim.ai/agent/v1
auth-host: https://test.us.auth0.com
name: test-agent
pool-labels:
  - small
aws-config-destination-dir: /container/aws
`)

	err = s.agent.LoadConfig()
	s.NoError(err)

	// Check that the AWS directory was detected and added to mounts
	s.Equal(awsDir, s.agent.HostAWSConfigDir)
	s.True(s.agent.HostAWSConfigExists)
	found := false
	for _, mount := range s.agent.CustomerWorkerConfig.Mounts {
		if mount.Source == awsDir && mount.Target == "/container/aws" {
			found = true
			break
		}
	}
	s.True(found, "AWS directory mount not found")
}

func (s *ConfigTestSuite) TestLoadConfigNoAWSDirectory() {

	// "Dependency Injection a la Golang"
	s.agent.getAWSConfigDirFunc = func() (string, bool) {
		return "", false
	}

	s.createConfigFile(`
api-host: https://test-api.resim.ai/agent/v1
auth-host: https://test.us.auth0.com
name: test-agent
pool-labels:
  - small
aws-config-destination-dir: /container/aws
`)

	err := s.agent.LoadConfig()
	s.NoError(err)

	// Check that the AWS directory was detected and added to mounts
	s.Empty(s.agent.HostAWSConfigDir)
	s.False(s.agent.HostAWSConfigExists)
	s.Len(s.agent.CustomerWorkerConfig.Mounts, 0)
}

func (s *ConfigTestSuite) TestLoadConfigAWSSourceOverride() {
	// Create mock custom AWS directory
	customAwsDir := filepath.Join(s.tempHomeDir, "custom-aws")
	err := os.MkdirAll(customAwsDir, 0700)
	s.NoError(err)

	// Also create the default .aws directory
	defaultAwsDir := filepath.Join(s.tempHomeDir, ".aws")
	err = os.MkdirAll(defaultAwsDir, 0700)
	s.NoError(err)

	// Set the mock function to return the default directory
	s.agent.getAWSConfigDirFunc = func() (string, bool) {
		return defaultAwsDir, true
	}

	s.createConfigFile(`
api-host: https://test-api.resim.ai/agent/v1
auth-host: https://test.us.auth0.com
name: test-agent
pool-labels:
  - small
aws-config-source-dir: ` + customAwsDir + `
aws-config-destination-dir: /container/aws
`)

	err = s.agent.LoadConfig()
	s.NoError(err)

	// Check that the custom AWS directory was used
	s.Equal(defaultAwsDir, s.agent.HostAWSConfigDir)
	s.True(s.agent.HostAWSConfigExists)
	found := false
	for _, mount := range s.agent.CustomerWorkerConfig.Mounts {
		if mount.Source == customAwsDir && mount.Target == "/container/aws" {
			found = true
			break
		}
	}
	s.True(found, "Custom AWS directory mount not found")
}

func (s *ConfigTestSuite) TestLoadConfigDockerNetworkMode() {
	// Test bridge mode (default)
	s.createConfigFile(`
api-host: https://test-api.resim.ai/agent/v1
auth-host: https://test.us.auth0.com
name: test-agent
pool-labels:
  - small
`)

	err := s.agent.LoadConfig()
	s.NoError(err)
	s.Equal(DockerNetworkModeBridge, s.agent.DockerNetworkMode)

	// Test host mode
	s.createConfigFile(`
api-host: https://test-api.resim.ai/agent/v1
auth-host: https://test.us.auth0.com
name: test-agent
pool-labels:
  - small
docker-network-mode: host
`)

	err = s.agent.LoadConfig()
	s.NoError(err)
	s.Equal(DockerNetworkModeHost, s.agent.DockerNetworkMode)
}

func TestParseNetworkMode(t *testing.T) {
	// Test valid network modes
	mode, err := parseNetworkMode("bridge")
	assert.NoError(t, err)
	assert.Equal(t, DockerNetworkModeBridge, mode)

	mode, err = parseNetworkMode("host")
	assert.NoError(t, err)
	assert.Equal(t, DockerNetworkModeHost, mode)
}
