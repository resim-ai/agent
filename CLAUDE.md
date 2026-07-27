# CLAUDE.md — agent

Repo-specific guidance only. Shared ReSim conventions (ecosystem map, PR policy, Linear/Wobblies workflow, comment policy, plan naming) currently live in the `resim-ai/workspace` coordination repo and will move into the `resim-shared` plugin (see docs/plans/2026-07-15-001). `resim-ai` is a *sibling*, not a parent, so its CLAUDE.md does NOT auto-load here — never reference it with a relative path.

## What this is

The ReSim Agent — a Go binary (`github.com/resim-ai/agent`, Go 1.26) that runs ReSim jobs on customer-controlled hosts to support Hardware-in-the-Loop (HiL) testing. It authenticates with the ReSim API, polls for jobs matching its `pool-labels`, and runs them as Docker containers on the host. Config loads from `~/.resim/config.yaml` by default. See `README.md` for the full config reference.

## Commands

All via `just` (see `Justfile`). Go 1.26 toolchain assumed.

- Build:   `go build -o ./resim-agent ./`
- Test:    `just test`            # `go test . -v -race`  (single method below)
- Lint:    `just lint`            # `golangci-lint run` — run BEFORE pushing (needs golangci-lint installed)
- Vet:     `just vet`             # `go vet`
- Codegen: `just generate`        # `go generate ./...` — regen `api/client.gen.go`; CI fails on stale generated files
- Coverage: `just test_coverage`  # `go test . -coverprofile=coverage.out`
- Colour test output: `just test_colour`  # same as `just test` via `grc` (needs grc installed)
- Integration: `just integration_test`  # `go test ./test/integration -v` — REQUIRES a configured environment (see Gotchas)

Single test method (testify suites — prefix with the suite runner, then `-testify.m`):

```
go test . -v -race -run TestAgentSuite -testify.m TestMethodName
```

## PRs

Uses **gh** — `git push` then `gh pr create`. This repo is Go but does **NOT** use Graphite; do not run `gt`.

Releases: push a `v*` tag to `main` (GitHub Actions builds and uploads binaries to Releases).

## Testing conventions

- Unit tests live at the repo root (`agent_test.go`, `config_test.go`) using **testify suites** (`github.com/stretchr/testify/suite`) plus plain table tests (e.g. `TestParseNetworkMode`). Run with race detection (`-race`).
- Suite runner functions: `TestAgentSuite` (`AgentTestSuite`), `TestConfigSuite` (`ConfigTestSuite`).
- Integration tests live in `test/integration/` (runner `TestAgentTestSuite`) and require a live ReSim environment, AWS credentials, a running/configured agent, and several `AGENT_TEST_*` env vars — they do not run without setup. See `test/integration/README.md`.

## Gotchas

- **gh, not Graphite** — never use `gt` here (see PRs above).
- **`just generate` hits the network.** Codegen runs `oapi-codegen` against the live OpenAPI spec at `https://agentapi.resim.ai/agent/v1/openapi.yaml` (config in `api/client.cfg.yml`). Regenerate `api/client.gen.go` after any agent-API change; needs network access.
- **`just lint` needs `golangci-lint`** installed locally (config in `.golangci.yml`); `just test_colour` needs `grc`.
- **`just integration_test` is environment-gated** — expects AWS profile `rerun_dev`, an agent config with a unique `pool-labels`, and the `AGENT_TEST_*` env vars from `test/integration/README.md`. `TestDockerAgentWithS3Experience` also needs a dockerized agent running.
- **`just env_kill ENV`** runs `terraform destroy` against a named integration dev environment — destructive; only for cleaning up integration test infra.
- `pool-labels` are OR/ANY matched: an agent with `big` and `small` picks up jobs tagged with either.

## Directory map

- `main.go` — entry point; agent implementation is `package main` at the repo root.
- `config.go`, `auth.go`, `update.go`, `docker_interface.go` — config loading, ReSim API auth, self-update, Docker container interface.
- `agent_test.go`, `config_test.go` — root-level unit tests (testify suites).
- `api/` — generated ReSim Agent API client: `client.gen.go` (generated), `generate.go` (`//go:generate` directive), `client.cfg.yml` (oapi-codegen config).
- `test/integration/` — integration tests, Terraform (`instance.tf`) to spin up an Ubuntu test host, `Dockerfile`, and cloud-init/CloudWatch `templates/`.
- `.github/workflows/` — CI: unit tests + codecov, integration tests, image build/push, release, security/SBOM.
- `Justfile` — task runner. `Dockerfile` — agent container image.
