//go:build generate
// +build generate

package api

// We import the codegen package so that go mod tidy doesn't remove it.
import (
	_ "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"
)

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config=client.cfg.yml https://agentapi.resim.ai/agent/v1/openapi.yaml
