package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/natefinch/lumberjack.v2"
)

type DockerNetworkMode string

const (
	DockerNetworkModeBridge DockerNetworkMode = "bridge"
	DockerNetworkModeHost   DockerNetworkMode = "host"
)

const (
	APIHostDefault                   = "https://agentapi.resim.ai/agent/v1"
	APIHostKey                       = "api-host"
	AuthHostDefault                  = "https://resim.us.auth0.com"
	AuthHostKey                      = "auth-host"
	PoolLabelsKey                    = "pool-labels"
	OneTaskKey                       = "one-task"
	UsernameKey                      = "username"
	PasswordKey                      = "password"
	AgentNameKey                     = "name"
	EnvPrefix                        = "RESIM_AGENT"
	LogLevelKey                      = "log-level"
	LogFilesizeKey                   = "log-max-filesize"
	LogFilesizeDefault               = 500
	AutoUpdateKey                    = "auto-update"
	PrivilegedKey                    = "privileged"
	PrivilegedDefault                = false
	NetworkModeKey                   = "docker-network-mode"
	NetworkModeDefault               = string(DockerNetworkModeBridge)
	ConfigPath                       = "$HOME/resim"
	CredentialCacheFilename          = "cache.json"
	CustomerContainerAWSDestDirKey   = "aws-config-destination-dir"
	CustomerContainerAWSSourceDirKey = "aws-config-source-dir"
	VolumeMountsKey                  = "mounts"
	EnvVarsKey                       = "environment-variables"
)

type CustomWorkerConfig struct {
	Mounts  []Mount  `json:"mounts"`
	EnvVars []EnvVar `json:"envvars"`
}

type Mount struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func parseNetworkMode(mode string) (DockerNetworkMode, error) {
	switch DockerNetworkMode(mode) {
	case DockerNetworkModeBridge, DockerNetworkModeHost:
		return DockerNetworkMode(mode), nil
	default:
		return DockerNetworkModeBridge, errors.New("invalid network mode")
	}
}

// GetHostAWSConfigDir returns the absolute path to an expected host AWS config dir
// and a boolean indicating whether it exists
func GetHostAWSConfigDir() (string, bool) {
	var homeDir string
	user, err := user.Current()
	if err != nil {
		slog.Warn("Couldn't lookup user; assuming root", "error", err)
		homeDir = "/root"
	} else {
		homeDir = user.HomeDir
	}
	// Now parse the deprecated aws config dirs into the customer worker config mounts:
	hostAWSConfigDir, _ := filepath.Abs(filepath.Join(homeDir, ".aws"))
	// check that this exists:
	configDirExists := true
	_, err = os.Stat(hostAWSConfigDir)
	if err != nil {
		slog.Info("AWS config directory does not exist")
		configDirExists = false
	}
	return hostAWSConfigDir, configDirExists
}

func (a *Agent) LoadConfig() error {
	configDir, err := a.GetConfigDir()
	if err != nil {
		slog.Error("error getting config dir", "err", err)
		return err
	}
	viper.SetConfigFile(filepath.Join(configDir, "config.yaml"))

	err = viper.ReadInConfig() // Find and read the config file
	if err != nil {            // Handle errors reading the config file
		return err
	}

	viper.SetEnvPrefix(EnvPrefix)
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	viper.SetDefault(LogLevelKey, "info")
	a.LogLevel = viper.GetString(LogLevelKey)

	viper.SetDefault(AutoUpdateKey, false)
	a.AutoUpdate = viper.GetBool(AutoUpdateKey)

	viper.SetDefault(LogFilesizeKey, LogFilesizeDefault)

	viper.SetDefault(PrivilegedKey, PrivilegedDefault)
	a.Privileged = viper.GetBool(PrivilegedKey)

	viper.SetDefault(NetworkModeKey, NetworkModeDefault)
	a.DockerNetworkMode, err = parseNetworkMode(viper.GetString(NetworkModeKey))
	if err != nil {
		log.Fatalf("Agent only supports %v or %v for docker network mode", DockerNetworkModeBridge, DockerNetworkModeHost)
	}

	viper.SetDefault(APIHostKey, APIHostDefault)
	viper.SetDefault(AuthHostKey, AuthHostDefault)

	a.APIHost = viper.GetString(APIHostKey)
	a.AuthHost = viper.GetString(AuthHostKey)
	if strings.HasSuffix(a.AuthHost, "/") {
		a.AuthHost = strings.TrimRight(a.AuthHost, "/")
	}
	if a.AuthHost != AuthHostDefault {
		a.ClientID = devClientID
	} else {
		a.ClientID = prodClientID
	}

	if !viper.IsSet(AgentNameKey) {
		log.Fatal("Agent name must be set")
	}
	a.Name = viper.GetString(AgentNameKey)

	if !viper.IsSet(PoolLabelsKey) {
		log.Fatal("Pool labels must be set")
	}
	a.PoolLabels = viper.GetStringSlice(PoolLabelsKey)

	//Parse mounts
	if viper.IsSet(VolumeMountsKey) {
		mountsString := viper.GetStringSlice(VolumeMountsKey)
		for _, mount := range mountsString {
			mountParts := strings.Split(mount, ":")
			if len(mountParts) != 2 {
				log.Fatal("Invalid mount format: must be <source>:<target>")
			}
			a.CustomerWorkerConfig.Mounts = append(a.CustomerWorkerConfig.Mounts, Mount{Source: mountParts[0], Target: mountParts[1]})
		}
	}

	// Look for a standard AWS config dir on the host:
	var hostAWSConfigDir string
	var configDirExists bool

	if a.getAWSConfigDirFunc != nil {
		// Use test override function
		hostAWSConfigDir, configDirExists = a.getAWSConfigDirFunc()
	} else {
		// Use real implementation
		hostAWSConfigDir, configDirExists = GetHostAWSConfigDir()
	}

	if configDirExists {
		a.HostAWSConfigDir = hostAWSConfigDir
		a.HostAWSConfigExists = true
	} else {
		a.HostAWSConfigExists = false
		slog.Warn("No AWS config dir found on host; will not mount AWS config dir for worker or container")
	}

	viper.SetDefault(CustomerContainerAWSDestDirKey, "")
	viper.SetDefault(CustomerContainerAWSSourceDirKey, hostAWSConfigDir)

	// Finally, if there is also a destination dir: add it to the mounts with the source dir::
	if viper.GetString(CustomerContainerAWSDestDirKey) != "" && viper.GetString(CustomerContainerAWSSourceDirKey) != "" {
		a.CustomerWorkerConfig.Mounts = append(a.CustomerWorkerConfig.Mounts,
			Mount{
				Source: viper.GetString(CustomerContainerAWSSourceDirKey),
				Target: viper.GetString(CustomerContainerAWSDestDirKey),
			},
		)
	}

	// parse env vars
	if viper.IsSet(EnvVarsKey) {
		envVarsString := viper.GetStringSlice(EnvVarsKey)
		for _, envVar := range envVarsString {
			envVarParts := strings.Split(envVar, "=")
			if len(envVarParts) != 2 {
				log.Fatal("Invalid environment variable format: must be <key>=<value>")
			}
			a.CustomerWorkerConfig.EnvVars = append(a.CustomerWorkerConfig.EnvVars, EnvVar{Key: envVarParts[0], Value: envVarParts[1]})
		}
	}

	slog.Info("loaded config",
		"apiHost", a.APIHost,
		"authHost", a.AuthHost,
		"name", a.Name,
		"poolLabels", a.PoolLabels,
		"privileged", a.Privileged,
		"dockerNetworkMode", a.DockerNetworkMode,
		"mounts", a.CustomerWorkerConfig.Mounts,
		"envVars", a.CustomerWorkerConfig.EnvVars,
		"one_task", viper.GetBool(OneTaskKey),
	)

	return nil
}

func (a *Agent) InitializeLogging() error {
	var logDir string
	if a.LogDirOverride != "" {
		logDir = a.LogDirOverride
	} else {
		userHomeDir, _ := os.UserHomeDir()
		logDir = filepath.Join(userHomeDir, "resim")
	}
	logFileWriter := &lumberjack.Logger{
		Filename:   fmt.Sprintf("%v/agent.log", logDir),
		MaxSize:    viper.GetInt(LogFilesizeKey),
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	// test write to check permissions on the file
	_, err := logFileWriter.Write([]byte(fmt.Sprintf("ReSim Agent %v", agentVersion)))
	if err != nil {
		return err
	}

	var slogLevel slog.Level
	switch a.LogLevel {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "error":
		slogLevel = slog.LevelError
	case "warn":
		slogLevel = slog.LevelWarn
	default:
		slog.Warn("invalid log level set in config")
		slogLevel = slog.LevelDebug
	}

	logWriters := io.MultiWriter(os.Stdout, logFileWriter)
	logHandler := slog.NewTextHandler(logWriters, &slog.HandlerOptions{Level: slogLevel})
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	return nil
}
