package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
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
)

func parseNetworkMode(mode string) (DockerNetworkMode, error) {
	switch DockerNetworkMode(mode) {
	case DockerNetworkModeBridge, DockerNetworkModeHost:
		return DockerNetworkMode(mode), nil
	default:
		return DockerNetworkModeBridge, errors.New("invalid network mode")
	}
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

	viper.SetDefault(CustomerContainerAWSDestDirKey, "")
	a.CustomerContainerAWSDestDir = viper.GetString(CustomerContainerAWSDestDirKey)

	viper.SetDefault(CustomerContainerAWSSourceDirKey, "")
	a.CustomerContainerAWSSourceDir = viper.GetString(CustomerContainerAWSSourceDirKey)

	slog.Info("loaded config",
		"apiHost", a.APIHost,
		"authHost", a.AuthHost,
		"name", a.Name,
		"poolLabels", a.PoolLabels,
		"privileged", a.Privileged,
		"dockerNetworkMode", a.DockerNetworkMode,
		"customerContainerAWSDestDir", a.CustomerContainerAWSDestDir,
		"customerContainerAWSSourceDir", a.CustomerContainerAWSSourceDir,
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
