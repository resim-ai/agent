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

Uses **gh** — `git push` then `gh pr create`; a large change goes up as a GitHub stacked-PR chain (`gh stack`).

Releases: push a `v*` tag to `main` (GitHub Actions builds and uploads binaries to Releases).

## Testing conventions

- Unit tests live at the repo root (`agent_test.go`, `config_test.go`) using **testify suites** (`github.com/stretchr/testify/suite`) plus plain table tests (e.g. `TestParseNetworkMode`). Run with race detection (`-race`).
- Suite runner functions: `TestAgentSuite` (`AgentTestSuite`), `TestConfigSuite` (`ConfigTestSuite`).
- Integration tests live in `test/integration/` (runner `TestAgentTestSuite`) and require a live ReSim environment, AWS credentials, a running/configured agent, and several `AGENT_TEST_*` env vars — they do not run without setup. See `test/integration/README.md`.

## Gotchas

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

<!-- resim-shared:begin writing-conventions (managed by the resim-shared plugin: edit defaults/claude-md/writing-conventions.md in resim-ai/workspace; its sync-shared-defaults workflow rewrites this copy) -->

## Writing conventions

Shared across ReSim repos. Each artifact has a different reader and a different lifetime, which is why the same context belongs in one of them and not the others. The `resim-shared` plugin carries the full policies (`resim-orient` → Code Comments, Never Post Comments Unsolicited) and `/comment-audit`, the sweep for a branch you iterated on.

### Code comments

Preserve useful existing comments, including multi-line documentation. Run instructions, test purpose, template or generated-code maintenance requirements, external contracts and non-obvious constraints all earn their place. Keep new comments concise, with enough detail to be useful.

Editing a file is not a reason to strip its comments. Remove comments only when they are stale, misleading, redundant, or merely narrate an edit. Correct outdated portions while retaining useful guidance; when uncertain, inspect the context and preserve the comment until there is a concrete reason to remove it.

Comments must describe the code itself. Do **not** reference ephemeral context that belongs in the PR description:

- Plan documents or sub-task labels (`see plan U5`, `Round 2 design`, `KTD`, `per the orchestration plan`).
- Linear ticket IDs (`WOB-4129`, `RSC-1159`), other PRs in a stack, or follow-up work that will land later.
- Measurements, timings and incident narration from the investigation that produced the change.
- Environment variables cited for narration rather than mechanics (`from AGENT_LATEST_KNOWN_VERSION`).
- Change-history narration such as "added for the Y flow" or "handles the case from issue #123". Durable usage instructions and caller constraints are useful documentation.

That context rots in code — PR descriptions are the right place for it. If a comment wouldn't make sense to a reader who has never seen the originating plan or PR, delete it.

Don't restate what well-named code already says. No `// returns the user` above `func GetUser()`.

Never leave a comment explaining the version you just replaced (`now uses X`, `no longer Y`, `per the design`): it narrates the edit, not the code.

### PR descriptions

Write for engineers and product owners who have not followed the conversation. Explain the feature, its purpose, and the decisions needed to review or roll it out. For a substantive change, aim for roughly **150–250 words**; simple changes can be shorter. This is a guide to useful depth, not a quota.

Use this order, without requiring a heading for each part:

1. **What changes and why:** open with one or two sentences naming the concrete capability or behavior and the problem it addresses. A short before/after example can help.
2. **The important pieces:** explain two to four feature-level changes when there are distinct pieces to understand. Short bullets with descriptive labels work well; do not force a list for a simple change.
3. **Why it matters:** connect those changes to the customer workflow or engineering capability they enable. Explain meaningful scope boundaries when they prevent a likely misunderstanding.
4. **Dependencies and verification, when relevant:** explain any actual merge or activation prerequisite, why it exists, and what it unblocks. Briefly state meaningful validation and remaining gaps; distinguish completed checks from planned checks.

Include technical terms only when they explain behavior, permissions, constraints, or a material tradeoff. Omit file/function inventories, implementation walkthroughs, commands and logs, investigation chronology, abandoned approaches, and conversational status updates. Do not include related-work lists or routine stack bookkeeping. Link another PR only when a reviewer needs it to understand an actual dependency; put that explanation after the feature summary, without a mandatory `Stack position` block or track number in the title.

Ground every claim in the final diff and verified evidence. Rewrite the title and body when scope changes. Keep the title about the delivered capability. Before finishing, check that a reader can explain **what this delivers, why we need it, and what must happen before it is usable** without reading the conversation. Use the shared `pr-describer` agent's example as a guide to depth and tone.

### Changelog entries

Changelog entries are for humans. State what changed and link the parent PR, plus the invariant or behaviour needed to make sense of it. Everything else belongs in the PR description. An entry is read later by someone deciding whether a version matters to them, not by someone reviewing the work.

<!-- resim-shared:end writing-conventions -->
