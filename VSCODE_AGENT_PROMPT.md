# ServerVault — Initial Agent Prompt

Continue development of:

`https://github.com/JamunaSoft/servervault`

Work on branch:

`go-rewrite`

Before changes:
1. Read `CLAUDE.md`, `AGENTS.md`, and `PROJECT_STATUS.md`.
2. Run `git status`.
3. Verify the branch.
4. Inspect Go and shell code.
5. Propose a concise milestone plan.

Goal: build `v0.2.0-alpha` foundation.

Order:
1. Move root Cobra wiring into `internal/cli`.
2. Add build metadata package.
3. Add YAML config model, loader, env overrides, validation.
4. Add `servervault config validate`.
5. Add `log/slog`.
6. Add non-destructive `servervault doctor`.
7. Add tests.
8. Add Go CI and Makefile.

Constraints:
- Go 1.22
- no secrets
- no destructive behavior
- business logic independent from Cobra
- contexts and wrapped errors
- small focused commits
- run formatting/tests/build

Do not implement the production backup engine until foundation is complete.
