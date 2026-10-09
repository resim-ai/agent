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
