package agent

import (
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

const (
	APIHostDefault          = "https://agentapi.resim.ai/agent/v1"
	APIHostKey              = "api-host"
	AuthHostDefault         = "https://resim.us.auth0.com"
	AuthHostKey             = "auth-host"
	PoolLabelsKey           = "pool-labels"
	OneTaskKey              = "one-task"
	UsernameKey             = "username"
	PasswordKey             = "password"
	AgentNameKey            = "name"
	EnvPrefix               = "RERUN_AGENT"
	LogLevelKey             = "log-level"
	ConfigPath              = "$HOME/resim"
	CredentialCacheFilename = "cache.json"
)

func (a *Agent) LoadConfig() error {
	viper.SetConfigName("config") // name of config file (without extension)
	viper.SetConfigType("yaml")   // REQUIRED if the config file does not have the extension in the name
	if a.ConfigDirOverride != "" {
		viper.AddConfigPath(a.ConfigDirOverride)
	} else {
		viper.AddConfigPath(ConfigPath)
	}

	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		return err
	}

	viper.SetEnvPrefix(EnvPrefix)
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	viper.SetDefault(LogLevelKey, "info")
	a.LogLevel = viper.GetString(LogLevelKey)

	viper.SetDefault(APIHostKey, APIHostDefault)
	viper.SetDefault(AuthHostKey, AuthHostDefault)

	a.APIHost = viper.GetString(APIHostKey)
	a.AuthHost = viper.GetString(AuthHostKey)
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

	slog.Info("loaded config",
		"apiHost", a.APIHost,
		"authHost", a.AuthHost,
		"name", a.Name,
		"poolLabels", a.PoolLabels,
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
		MaxSize:    500,
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	// test write to check permissions on the file
	_, err := logFileWriter.Write([]byte("ReSim Agent Log"))
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
