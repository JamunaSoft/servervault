# Testing

## Principles

- Prefer table-driven tests for pure functions (config parsing,
  validation, retention math).
- Test failure paths, not just the happy path — a backup tool's most
  important behavior is what it does when something goes wrong (see
  [`docs/security-model.md`](security-model.md)).
- The default `go test ./...` run (no build tags) never requires a real
  Restic repository, a real PostgreSQL server, or network access.
  External commands are invoked through `internal/execx`; tests
  substitute a fake `execx.Runner` rather than shelling out. Real-binary
  coverage lives in the separate, opt-in **integration** suite — see
  below.
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

This is the default, always-run suite: fast, no external dependencies,
runs identically on a laptop or in CI regardless of what's installed.

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

## Core infrastructure tests (`internal/job`, `internal/scheduler`, `internal/event`)

These three packages (see [`docs/core-infrastructure.md`](core-infrastructure.md))
are part of the default, always-run `go test ./...` suite -- no build tag,
no real restic/PostgreSQL dependency, no external service. A few of their
tests are worth calling out because they exercise real behavior rather
than just asserting against fakes:

- **`internal/job`'s `TestStore_ReconcileAfterUncleanRestart`** is a real
  crash-consistency test, not a simulated one: it spawns this same test
  binary as a subprocess (the standard Go `-test.run=TestHelperProcess_*`
  pattern), has that subprocess create a job, advance it into a
  non-terminal state, and send itself `SIGKILL` -- no deferred `Close`,
  no graceful shutdown. The outer test then reopens the same SQLite file
  in-process and asserts it isn't corrupted and that `Reconcile` marks
  the orphaned job `interrupted`. Skipped on non-unix platforms (`SIGKILL`
  has no equivalent). No sleeps are used to synchronize -- the test waits
  on `cmd.Wait()`, and the subprocess's own brief `time.Sleep` before
  killing itself is a safety margin, not a correctness requirement (the
  write it's protecting already completed synchronously before that
  sleep).
- **`internal/job`'s concurrency tests**
  (`TestStore_Advance_ConcurrentUpdatesAreSerializedSafely`,
  `TestStore_Advance_ConcurrentDifferentJobs`) fire many real goroutines
  at the same `Store` and assert exactly one wins a same-row race while
  independent rows never contend -- run under `-race` in CI like every
  other package.
- **`internal/scheduler`'s DST tests**
  (`TestSchedule_NextRun_DST_SpringForward`,
  `TestSchedule_NextRun_DST_FallBack`) assert against a real
  `America/New_York` transition date via the IANA timezone database
  rather than a synthetic offset, and skip cleanly if that timezone data
  isn't installed in the environment running the test.
- **Both `internal/job.Metadata` and `internal/event.Metadata`** carry a
  reflection-based regression test
  (`TestMetadata_NoSecretShapedFields`) that fails the build if a future
  change ever adds a field whose name looks like it could hold a secret
  -- see [`docs/core-infrastructure.md`](core-infrastructure.md#safety-no-secrets-in-persisted-state).

## Shell implementation

The shell scripts don't have a unit test suite; correctness is enforced
by `bash -n` (syntax) and `shellcheck` (lint) in CI
(`.github/workflows/shell.yml`), plus the built-in self-verification
each script performs (dump verification in `servervault-backup`,
`restic check` in `servervault-verify`).

## Integration tests

The backup engine (`internal/lock`, `internal/restic`, `internal/postgres`,
`internal/backup`) has a second, separate test suite that runs real
`restic`/`pg_dump`/`pg_restore`/`psql`/`zstd` against temporary, disposable
resources — never against a production repository, database, or
credentials. It is gated behind the `integration` build tag, so it's
never part of a plain `go test ./...` run:

```bash
go test -tags=integration -race ./...
# or
make test-integration
```

### What's covered

| Area | File | Real backend |
| --- | --- | --- |
| Backup, check, snapshots, `cat config` | `internal/restic/integration_test.go` | `restic` against a fresh `local:` repository in `t.TempDir()` |
| Wrong repository password | same | same |
| Ping, dump, verify | `internal/postgres/integration_test.go` | `pg_dump`/`pg_restore`/`psql` against a disposable `servervault_test_*` database |
| Corrupted dump detection + cleanup | same | same |
| End-to-end backup (Postgres on/off), cancellation, cleanup after every failure mode, concurrent-lock | `internal/backup/integration_test.go`, `internal/backup/concurrency_test.go` | both of the above together |

`internal/backup/concurrency_test.go` (the concurrent-lock test) has **no
build tag** — it always runs as part of the normal unit suite. Concurrency
correctness is a property of `internal/lock`'s real `flock` usage and
`Engine.Run`'s own control flow, not of restic/postgres's behavior, so it
doesn't need — and shouldn't wait on — a real backend.

### Environment requirements

| Tool | Needed for |
| --- | --- |
| `restic` | the restic-backed tests above |
| `postgresql` (server) + `postgresql-client` (`pg_dump`, `pg_restore`, `psql`) | the postgres-backed tests above — a *local server*, not just client tools, because PostgreSQL peer authentication (see [`docs/configuration.md`](configuration.md)) requires a real local Unix socket and OS role, which a remote/containerized Postgres wouldn't provide |
| `sudo`, non-interactive, for the configured test role | creating/dropping the disposable test database |

`SERVERVAULT_TEST_POSTGRES_USER` overrides which OS/database role the
PostgreSQL tests authenticate as (defaults to `postgres`). CI uses a
dedicated, low-privilege role created for this purpose rather than the
`postgres` superuser — see the CI section below.

### Skip behavior

Every integration test skips (`t.Skip`, not `t.Fatal`) when a
prerequisite isn't available locally:

- `restic` not on `PATH` → restic-backed tests skip.
- `pg_dump`/`pg_restore`/`psql` not on `PATH` → postgres-backed tests
  skip.
- Binaries present but the disposable test database can't be created
  (no privilege, no local server) → postgres-backed tests skip.

This is a **local-developer-experience** guarantee, not a CI one: CI jobs
that explicitly install `restic`/`postgresql` verify the install
succeeded (a dedicated `restic version` / connectivity step) before
running tests, so a broken CI setup fails the job instead of silently
reporting a pass via skipped tests.

### Safety

- Every Restic repository is a fresh `local:<t.TempDir()>` backend with a
  randomly generated, test-only password — there is no code path in the
  integration suite that reads a real `servervault.yaml` or a real
  password file.
- Every PostgreSQL database is named `servervault_test_<random>`; the
  helper that drops it (`internal/testsupport.NewPostgresDatabase`)
  refuses to run against any name without that prefix, even if called
  incorrectly.
- Nothing is ever created outside `t.TempDir()` except the disposable
  database itself, which is dropped via `t.Cleanup` (runs on failure and
  panic, not just success).
- No Docker: a local `restic` binary plus a local `postgresql` install are
  sufficient, and — for PostgreSQL specifically — are the *only* way to
  exercise the real peer-auth code path (a container reachable over TCP
  wouldn't exercise `sudo -u <user>` over a Unix socket at all).

### The restic lock-conflict probe (opt-in, not required)

`internal/restic/lockprobe_test.go` is a **separate, best-effort** probe
of restic's own repository-locking behavior under real concurrent
`restic backup` invocations. It is gated behind an *additional* build tag
on top of `integration`:

```bash
go test -tags=integration,resticlock -race ./internal/restic/...
# or
make test-resticlock
```

It is **not** part of `test-integration` and is not required for normal
pull requests — see the CI section below. Two reasons:

1. **Version-sensitive by construction.** It polls the local repository's
   on-disk `locks/` directory to know precisely when a background backup
   has acquired restic's internal lock — that directory's layout is an
   implementation detail of restic's `local:` backend, not a documented
   API. It also depends on restic's own lock-retry behavior, which isn't
   something ServerVault controls or assumes a specific version of. The
   test skips (doesn't fail) if it can't reliably observe a conflict.
2. **Deterministic coverage of the same *classification* logic already
   exists** as a normal, always-run unit test:
   `internal/restic/exitcode_test.go` (`TestClassify`) and
   `TestRepository_Check_LockConflictIsClassifiedDeterministically` in
   `restic_test.go` prove restic exit code 11 is classified as
   `ExitLockFailed`, using a fake `execx.Runner` — no real binary, no
   timing, no flakiness. The probe only adds confidence that a *real*
   restic process actually produces that exit code under contention; it
   is a compatibility check, not a correctness requirement.

## CI

- `.github/workflows/go.yml` — unit suite only (`gofmt -l`, `go vet`,
  `go test -race -cover`, `go build`) on every push/PR touching Go files.
  Unaffected by any of the above; build-tagged files are excluded by
  default.
- `.github/workflows/shell.yml` — shell syntax/lint, unaffected.
- `.github/workflows/integration.yml` — two jobs, on the same push/PR
  triggers as `go.yml`:
  - **`restic-integration`** (required/blocking): `sudo apt-get install
    -y restic`, verifies with `restic version`, then runs
    `go test -tags=integration -race ./...`. No PostgreSQL is installed
    in this job, so postgres-backed tests skip here by the same
    local-developer skip logic described above — deliberate, not a gap:
    it's what keeps this job requiring only `restic`.
  - **`postgres-integration`**: `sudo apt-get install -y postgresql
    postgresql-client`, starts the cluster and waits for `pg_isready`,
    creates a disposable OS user and PostgreSQL role named
    `servervault_test` with `CREATEDB` (not superuser), verifies
    connectivity, then runs `go test -tags=integration -race
    ./internal/postgres/... ./internal/backup/...` with
    `SERVERVAULT_TEST_POSTGRES_USER=servervault_test`. Ran with
    `continue-on-error: true` while its setup was being stabilized; that's
    since been removed now that the fixed job (cluster start/readiness
    wait, idempotent role creation) has run green. Still not yet added as
    a required status check in GitHub branch protection — give it a few
    more runs first.
- `.github/workflows/restic-lock-probe.yml` — the opt-in probe above,
  triggered by `workflow_dispatch` (manual) and a weekly `schedule`
  only — never on push or pull_request.

### Installing the prerequisites yourself

Locally (Debian/Ubuntu), matching what CI does:

```bash
sudo apt-get update
sudo apt-get install -y restic postgresql postgresql-client
```

After that, `postgres` is a real local OS/database role via the default
install, so `make test-integration` should exercise the PostgreSQL-backed
tests too (using `SERVERVAULT_TEST_POSTGRES_USER=postgres`, the default).
