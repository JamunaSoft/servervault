# Development

For contribution workflow and required checks, see
[`CONTRIBUTING.md`](../CONTRIBUTING.md). This page covers day-to-day
local development.

## Prerequisites

- Go 1.22+
- `shellcheck`, for shell changes
- `restic`, `zstd`, `postgresql-client` — only needed if you're
  exercising the tooling end-to-end, not for `go build`/`go test`

## Clone and branch

```bash
git clone https://github.com/JamunaSoft/servervault.git
cd servervault
git checkout go-rewrite
```

## Build and run

```bash
go build -o servervault ./cmd/servervault
./servervault version
```

Or with `make` / `task` (see [`Makefile`](../Makefile) and
[`Taskfile.yml`](../Taskfile.yml)):

```bash
make build && ./servervault version
# or
task build && ./servervault version
```

## Test

```bash
go test ./...
```

Tests should be table-driven where practical (see
[`docs/testing.md`](testing.md)) and must not require network access,
a real Restic repository, or a real PostgreSQL instance to pass —
external dependencies are mocked/faked behind interfaces in
`internal/execx`, `internal/restic`, and `internal/postgres`.

## Format and vet

```bash
gofmt -w .
go vet ./...
```

CI (`.github/workflows/go.yml`) rejects any diff that isn't already
`gofmt`-clean, so run this before committing.

## Working on the shell implementation

Shell changes go against `main`, not `go-rewrite`:

```bash
git checkout main
bash -n bin/* install.sh
shellcheck bin/* install.sh
```

## Editor setup

`.vscode/` ships recommended extensions, format-on-save settings, and
tasks (`Terminal > Run Task`) for `go: build`, `go: test`, `go: vet`,
and `shell: shellcheck`. `.vscode/launch.json` has debug configurations
for `servervault version`, `servervault doctor`, and the current
package's tests.

## Useful one-liners

```bash
# Everything CI checks, locally, in one shot:
make verify        # or: task verify

# Run only the tests for one package, verbosely:
go test -v ./internal/config/...
```

## Where things live

See [`docs/repository-layout.md`](repository-layout.md) and
[`docs/architecture.md`](architecture.md) for the package structure and
design rules new code is expected to follow.
