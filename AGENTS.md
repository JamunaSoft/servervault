# AGENTS.md — ServerVault

Instructions for Codex and other coding agents.

## Mission
Build a safe production-grade Linux backup and disaster recovery CLI.

## Read first
- `PROJECT_STATUS.md`
- `CLAUDE.md`
- `README.md`
- existing shell implementation

## Active branch
Use `go-rewrite` for new Go work. Keep `main` stable.

## Workflow
1. Run `git status`.
2. Confirm branch `go-rewrite`.
3. Inspect existing code.
4. Give a short implementation plan.
5. Make the smallest coherent change.
6. Format and test.
7. Summarize changed files and risks.

## Before commit

```bash
gofmt -w .
go test ./...
go vet ./...
go build ./cmd/servervault
```

## Never expose
- Restic passwords
- SSH keys
- Storage Box passwords
- DB passwords
- `.env`
- tokens
- production connection strings

## Never do automatically
- delete a repository
- prune without validated retention
- restore over live data
- drop a production DB
- delete staging before snapshot success
- continue after unexpected repository identity change

## Design rules
- business logic independent from Cobra
- use contexts
- wrap errors
- avoid globals
- use interfaces around external commands
- test failure paths
- keep output actionable

## Current task
Implement:
- root CLI
- YAML/environment config
- validation
- doctor
- structured logging
- build metadata
- tests
- CI
