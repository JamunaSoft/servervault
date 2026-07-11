# Testing

## Principles

- Prefer table-driven tests for pure functions (config parsing,
  validation, retention math).
- Test failure paths, not just the happy path — a backup tool's most
  important behavior is what it does when something goes wrong (see
  [`docs/security-model.md`](security-model.md)).
- No test should require a real Restic repository, a real PostgreSQL
  server, or network access. External commands are invoked through
  `internal/execx`; tests substitute a fake executor rather than
  shelling out.
- Keep platform-specific behavior isolated so most tests can run on any
  `GOOS` the CI matrix uses.

## Running tests

```bash
go test ./...
```

With race detection and coverage, matching CI:

```bash
go test -race -cover ./...
```

A single package, verbosely:

```bash
go test -v ./internal/config/...
```

## Table-driven test shape

```go
func TestValidateRetention(t *testing.T) {
	tests := []struct {
		name    string
		daily   int
		weekly  int
		monthly int
		wantErr bool
	}{
		{name: "valid", daily: 7, weekly: 4, monthly: 12},
		{name: "negative daily", daily: -1, weekly: 4, monthly: 12, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRetention(tt.daily, tt.weekly, tt.monthly)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRetention() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

## Fixtures

Test fixtures (sample YAML configs, sample `restic snapshots --json`
output, etc.) belong in `testdata/`, following Go's convention that the
`go` tool ignores directories named `testdata`.

## Shell implementation

The shell scripts don't have a unit test suite; correctness is enforced
by `bash -n` (syntax) and `shellcheck` (lint) in CI
(`.github/workflows/shell.yml`), plus the built-in self-verification
each script performs (dump verification in `servervault-backup`,
`restic check` in `servervault-verify`).

## CI

`.github/workflows/go.yml` runs `gofmt -l`, `go vet`, `go test -race
-cover`, and `go build` on every push and pull request touching Go
files. `.github/workflows/shell.yml` runs the shell equivalent.
