package main

import (
	"os"

	"github.com/resim-ai/agent"
)

func main() {
	a := agent.Agent{}

	err := a.Start()
	if err != nil {
		os.Exit(1)
	}
}
