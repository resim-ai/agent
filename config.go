package agent

import (
	"fmt"
	"log"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

const (
	APIHostDefault          = "https://agentapi.resim.ai/agent/v1"
	APIHostKey              = "api-host"
	AuthHostDefault         = "https://resim.us.auth0.com"
	AuthHostKey             = "auth-host"
	ClientIDDefault         = devClientID // TODO default to prod
	ClientIDKey             = "client-id"
	PoolLabelsKey           = "pool-labels"
	UsernameKey             = "username"
	PasswordKey             = "password"
	AgentNameKey            = "name"
	EnvPrefix               = "RERUN_AGENT"
	LogLevelKey             = "log-level"
	ConfigPath              = "$HOME/resim"
	CredentialCacheFilename = "cache.json"
)

func (a *Agent) loadConfig() error {
	viper.SetConfigName("config") // name of config file (without extension)
	viper.SetConfigType("yaml")   // REQUIRED if the config file does not have the extension in the name
	if a.ConfigFileOverride != "" {
		configDir, configFile := filepath.Split(a.ConfigFileOverride)
		viper.AddConfigPath(configDir)
		viper.SetConfigName(configFile)
	}

	viper.AddConfigPath(ConfigPath) // call multiple times to add many search paths
	err := viper.ReadInConfig()     // Find and read the config file
	if err != nil {                 // Handle errors reading the config file
		panic(fmt.Errorf("fatal error in config file: %w", err))
	}

	viper.SetEnvPrefix(EnvPrefix)
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	viper.SetDefault(LogLevelKey, "0") // info, default
	// TODO: work out how to convert strings into level numbers
	slog.SetLogLoggerLevel(slog.LevelDebug)

	viper.SetDefault(APIHostKey, APIHostDefault)
	viper.SetDefault(AuthHostKey, AuthHostDefault)
	viper.SetDefault(ClientIDKey, ClientIDDefault)

	a.APIHost = viper.GetString(APIHostKey)
	a.AuthHost = viper.GetString(AuthHostKey)
	a.ClientID = viper.GetString(ClientIDKey)

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
