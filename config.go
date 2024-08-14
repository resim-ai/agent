package agent

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/viper"
)

const (
	ApiHostDefault          = "https://api.resim.ai/v1"
	ApiHostKey              = "api-host"
	AuthHostDefault         = "https://resim.us.auth0.com"
	AuthHostKey             = "auth-host"
	EnvPrefix               = "RERUN_AGENT"
	LogLevelKey             = "log-level"
	ConfigPath              = "$HOME/resim"
	CredentialCacheFilename = "cache.json"
)

func (a *Agent) loadConfig() error {
	viper.SetConfigName("config")   // name of config file (without extension)
	viper.SetConfigType("yaml")     // REQUIRED if the config file does not have the extension in the name
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

	viper.SetDefault(ApiHostKey, ApiHostDefault)
	viper.SetDefault(AuthHostKey, AuthHostDefault)

	a.ApiHost = viper.GetString(ApiHostKey)
	a.AuthHost = viper.GetString(AuthHostKey)

	slog.Info("loaded config",
		"apiHost", a.ApiHost,
		"authHost", a.AuthHost,
	)

	return nil
}
