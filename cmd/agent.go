package main

import (
	"os"

	"github.com/resim-ai/agent"
)

func main() {
	a := agent.Agent{}

	ConfigDir := os.Getenv("RESIM_AGENT_CONFIG_DIR")
	if ConfigDir != "" {
		a.ConfigDirOverride = ConfigDir
	}

	LogDir := os.Getenv("RESIM_AGENT_LOG_DIR")
	if LogDir != "" {
		a.LogDirOverride = LogDir
	}

	err := a.Start()
	if err != nil {
		os.Exit(1)
	}
}
