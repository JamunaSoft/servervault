package restore

import (
	"errors"
	"fmt"
)

// ErrSnapshotNotFound is returned by Plan when the requested snapshot ID
// (unscoped -- no --path given) doesn't resolve to any snapshot in the
// repository at all. Distinct from ErrSnapshotPathNotFound, which means
// the snapshot itself was found but a --path scope within it wasn't.
var ErrSnapshotNotFound = errors.New("restore: no matching snapshot found")

// ErrSnapshotPathNotFound is returned by Plan when a --path scope doesn't
// match any entry in the snapshot.
var ErrSnapshotPathNotFound = errors.New("restore: no matching path found in snapshot")

// ErrDumpNotFound is returned by Plan for a temp-db restore when no
// PostgreSQL dump file exists in the snapshot's configured dump
// directory -- e.g. the snapshot was taken with PostgreSQL disabled.
var ErrDumpNotFound = errors.New("restore: no PostgreSQL dump file found in this snapshot")

// ErrAmbiguousDump is returned by Plan for a temp-db restore when more
// than one dump file is found where exactly one was expected -- Plan
// refuses to guess which one to restore.
var ErrAmbiguousDump = errors.New("restore: multiple PostgreSQL dump files found in this snapshot; refusing to guess which one to restore")

// ErrDestinationExists is returned when a computed destination (a
// staging directory or, at Execute time, a database name) already
// exists -- Plan and Execute both refuse to proceed rather than restore
// into or over something already there.
var ErrDestinationExists = errors.New("restore: destination already exists")

// ErrPlanStale is returned by Execute when a critical assumption the
// Plan was built on no longer holds by the time Execute actually runs
// (see Executor.Execute's revalidation step) -- e.g. the destination
// directory or database was created by something else between Plan and
// Execute.
type ErrPlanStale struct {
	Reason string
}

func (e *ErrPlanStale) Error() string {
	return fmt.Sprintf("restore: plan is no longer valid: %s", e.Reason)
}

// ErrDatabaseDisabled is returned by Plan for a temp-db restore when
// PostgreSQL is not enabled in configuration.
var ErrDatabaseDisabled = errors.New("restore: postgres is not enabled in configuration; cannot plan a database restore")

// ErrUnknownDatabase is returned by Plan when a --database value is
// given that doesn't match any configured database. In v0.4.0-alpha.1
// there is exactly one configured database (postgres.database); this
// error and the --database flag's validation shape are what let v0.5.0's
// multi-database model add more legal values without changing the CLI
// interface.
type ErrUnknownDatabase struct {
	Requested string
}

func (e *ErrUnknownDatabase) Error() string {
	return fmt.Sprintf("restore: unknown database %q", e.Requested)
}
