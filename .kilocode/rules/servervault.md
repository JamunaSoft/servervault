# ServerVault Kilo Code Rules

Branch for Go work: `go-rewrite`

Read:
- `PROJECT_STATUS.md`
- `CLAUDE.md`
- `AGENTS.md`

Rules:
- Keep `main` stable.
- Never commit secrets.
- Avoid destructive behavior.
- Restore to staging by default.
- Use Go 1.22 compatibility.
- Cobra only for CLI wiring.
- Use `log/slog`.
- Use `context.Context`.
- Run `gofmt`, `go test`, `go vet`, and `go build`.

Priority:
1. config loader and validation
2. doctor
3. logging
4. build metadata
5. tests and CI

Do not implement the Go backup engine yet.
