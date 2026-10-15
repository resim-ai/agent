# CLAUDE.md — agent

Repo-specific guidance only. Shared ReSim conventions (ecosystem map, PR policy, Linear/Wobblies workflow, comment policy, plan naming) ship in the `resim-shared` plugin, which `.claude/settings.json` enables; the `## Writing conventions` block at the end of this file is the plugin's always-loaded summary. `resim-ai` is a *sibling*, not a parent, so its CLAUDE.md does NOT auto-load here — never reference it with a relative path.

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

<!-- resim-shared:begin writing-conventions (managed by the resim-shared plugin: edit defaults/claude-md/writing-conventions.md in resim-ai/workspace, then run bin/resim-sync --apply) -->
## Writing conventions

Shared across ReSim repos. Each artifact has a different reader and a different lifetime, which is why the same context belongs in one of them and not the others. The `resim-shared` plugin carries the full policies (`resim-orient` → Code Comments, Never Post Comments Unsolicited) and `/comment-audit`, the sweep for a branch you iterated on.

### Code comments

Default to no comments. Add one only when the WHY is non-obvious — a hidden constraint, a workaround for a specific bug, or a non-intuitive invariant a reader would otherwise miss.

Comments must describe the code itself. Do **not** reference ephemeral context that belongs in the PR description:

- Plan documents or sub-task labels (`see plan U5`, `Round 2 design`, `KTD`, `per the orchestration plan`).
- Linear ticket IDs (`WOB-4129`, `RSC-1159`), other PRs in a stack, or follow-up work that will land later.
- Measurements, timings and incident narration from the investigation that produced the change.
- Environment variables cited for narration rather than mechanics (`from AGENT_LATEST_KNOWN_VERSION`).
- "Used by X", "added for the Y flow", "handles the case from issue #123".

That context rots in code — PR descriptions are the right place for it. If a comment wouldn't make sense to a reader who has never seen the originating plan or PR, delete it.

Don't restate what well-named code already says. No `// returns the user` above `func GetUser()`.

Never leave a comment explaining the version you just replaced (`now uses X`, `no longer Y`, `per the design`): it narrates the edit, not the code.

### PR descriptions

The PR description is the home for everything the other two exclude: how the problem was found, measurements and timings, affected resource names, ticket IDs, rollout and verification steps.

For a stacked or multi-track change, open with a `## Stack position` block near the top — not buried — stating which track it is, what it depends on, what gates it at merge time, and what it unblocks downstream.

### Changelog entries

Changelog entries are for humans. State what changed and link the parent PR, plus the invariant or behaviour needed to make sense of it. Everything else belongs in the PR description. An entry is read later by someone deciding whether a version matters to them, not by someone reviewing the work.
<!-- resim-shared:end writing-conventions -->
