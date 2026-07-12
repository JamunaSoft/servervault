package postgres

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JamunaSoft/servervault/internal/execx"
)

// RestoreError wraps a failure at a specific stage of RestoreToTemp.
type RestoreError struct {
	// Stage is one of "exists-check", "create", "decompress", "validate",
	// "restore", "connectivity-check".
	Stage string
	Err   error
}

func (e *RestoreError) Error() string {
	return fmt.Sprintf("postgres: restore (%s): %v", e.Stage, e.Err)
}
func (e *RestoreError) Unwrap() error { return e.Err }

var dangerousDatabaseNameChars = dangerousFilenameChars // reuse dump.go's allow-list regexp

// validDatabaseName reports whether name contains only characters safe to
// interpolate into a `CREATE DATABASE`/`DROP DATABASE` SQL identifier and
// into shell-free argv (createdb/dropdb/psql already receive it as a
// single argv element, never concatenated into a command string -- this
// check is defense in depth against a database name confusing psql's own
// identifier parsing, not a shell-injection concern, since internal/
// execx never invokes a shell).
func validDatabaseName(name string) bool {
	if name == "" {
		return false
	}
	return !dangerousDatabaseNameChars.MatchString(name)
}

// DatabaseExists reports whether a database named name already exists.
func (c *Client) DatabaseExists(ctx context.Context, name string) (bool, error) {
	if !validDatabaseName(name) {
		return false, fmt.Errorf("postgres: database name %q contains characters outside [A-Za-z0-9_.-]", name)
	}

	args := append(c.baseArgs(), "-d", "postgres", "-Atc",
		"SELECT 1 FROM pg_database WHERE datname = '"+name+"'")
	// name is validated above to contain only [A-Za-z0-9_.-], so this
	// string-built SQL fragment cannot contain a quote, semicolon, or
	// any other character that would let it escape the literal -- see
	// validDatabaseName. This is the one place in this package that
	// builds a SQL string rather than using an argv-only interface,
	// because psql has no client-side parameter-binding mode for -c/-tc;
	// the character allow-list is what makes it safe.
	pgName, pgArgs := c.commandFor("psql", args)

	var stdout, stderr bytes.Buffer
	err := c.runner.Run(ctx, execx.RunOptions{Name: pgName, Args: pgArgs, Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return false, &RestoreError{Stage: "exists-check", Err: wrapWithStderr(err, stderr)}
	}
	return strings.TrimSpace(stdout.String()) == "1", nil
}

// CreateDatabase creates a new, empty database named name. It first
// checks that no database with that name already exists (RestoreError
// with Stage "exists-check" if the check itself fails, a plain error if
// the database is already present) -- CreateDatabase never overwrites or
// reuses an existing database, matching CLAUDE.md's "never overwrite a
// live database by default" rule extended to restore targets in general.
// createdb itself provides the actual atomicity guarantee (it fails if
// the database already exists even if this pre-check raced with another
// creator); the pre-check exists to produce a clear, specific error
// rather than relying solely on createdb's generic failure text.
func (c *Client) CreateDatabase(ctx context.Context, name string) error {
	if !validDatabaseName(name) {
		return fmt.Errorf("postgres: database name %q contains characters outside [A-Za-z0-9_.-]", name)
	}

	exists, err := c.DatabaseExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("postgres: database %q already exists -- refusing to reuse or overwrite it", name)
	}

	args := append(c.baseArgs(), name)
	cmdName, cmdArgs := c.commandFor("createdb", args)

	var stderr bytes.Buffer
	if err := c.runner.Run(ctx, execx.RunOptions{Name: cmdName, Args: cmdArgs, Stderr: &stderr}); err != nil {
		return &RestoreError{Stage: "create", Err: wrapWithStderr(err, stderr)}
	}
	return nil
}

// DropDatabase drops the database named name. Callers must only ever call
// this for a database ServerVault itself created in the same operation
// (see internal/restore's ownership tracking) -- this package enforces
// nothing about that beyond the name-validity check; the "never drop a
// database ServerVault didn't create" guarantee lives in the caller.
func (c *Client) DropDatabase(ctx context.Context, name string) error {
	if !validDatabaseName(name) {
		return fmt.Errorf("postgres: database name %q contains characters outside [A-Za-z0-9_.-]", name)
	}

	args := append(c.baseArgs(), "--if-exists", name)
	cmdName, cmdArgs := c.commandFor("dropdb", args)

	var stderr bytes.Buffer
	if err := c.runner.Run(ctx, execx.RunOptions{Name: cmdName, Args: cmdArgs, Stderr: &stderr}); err != nil {
		return &RestoreError{Stage: "create", Err: wrapWithStderr(err, stderr)}
	}
	return nil
}

// PingDatabase verifies connectivity to database name specifically, with
// `SELECT 1` -- the same check Ping performs against cfg.Database, but
// parameterized so a caller (internal/restore, after restoring into a
// freshly created temporary database) can verify the new database is
// actually reachable rather than only checking it exists.
func (c *Client) PingDatabase(ctx context.Context, name string) error {
	if !validDatabaseName(name) {
		return fmt.Errorf("postgres: database name %q contains characters outside [A-Za-z0-9_.-]", name)
	}

	args := append(c.baseArgs(), "-d", name, "-Atc", "SELECT 1")
	cmdName, cmdArgs := c.commandFor("psql", args)

	var stdout, stderr bytes.Buffer
	err := c.runner.Run(ctx, execx.RunOptions{Name: cmdName, Args: cmdArgs, Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return &RestoreError{Stage: "connectivity-check", Err: wrapWithStderr(err, stderr)}
	}
	got := strings.TrimSpace(stdout.String())
	if got != "1" {
		return &RestoreError{Stage: "connectivity-check", Err: fmt.Errorf("unexpected response %q", got)}
	}
	return nil
}

// RestoreToTemp decompresses dumpPath, validates it (the same `pg_restore
// --list` check VerifyDump performs), and restores it into databaseName
// -- which the caller must already have created via CreateDatabase, empty
// and freshly-created for this operation, never a pre-existing or live
// database. The decompressed copy's lifetime is entirely scoped to this
// call, removed on every path including a mid-restore failure, the same
// pattern VerifyDump uses.
//
// RestoreToTemp never touches any database other than databaseName -- it
// has no code path that accepts or defaults to the live database name.
func (c *Client) RestoreToTemp(ctx context.Context, dumpPath, databaseName string) error {
	if !validDatabaseName(databaseName) {
		return fmt.Errorf("postgres: database name %q contains characters outside [A-Za-z0-9_.-]", databaseName)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dumpPath), "restore-*.dump")
	if err != nil {
		return &RestoreError{Stage: "decompress", Err: err}
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()

	var zstdStderr bytes.Buffer
	if err := c.runner.Run(ctx, execx.RunOptions{
		Name: "zstd", Args: []string{"-dc", dumpPath}, Stdout: tmp, Stderr: &zstdStderr,
	}); err != nil {
		return &RestoreError{Stage: "decompress", Err: wrapWithStderr(err, zstdStderr)}
	}

	var listStderr bytes.Buffer
	if err := c.runner.Run(ctx, execx.RunOptions{
		Name: "pg_restore", Args: []string{"--list", tmpPath}, Stderr: &listStderr,
	}); err != nil {
		return &RestoreError{Stage: "validate", Err: wrapWithStderr(err, listStderr)}
	}

	restoreArgs := append(c.baseArgs(), "--no-owner", "--no-privileges", "--dbname="+databaseName, tmpPath)
	cmdName, cmdArgs := c.commandFor("pg_restore", restoreArgs)

	var restoreStderr bytes.Buffer
	if err := c.runner.Run(ctx, execx.RunOptions{Name: cmdName, Args: cmdArgs, Stderr: &restoreStderr}); err != nil {
		return &RestoreError{Stage: "restore", Err: wrapWithStderr(err, restoreStderr)}
	}

	return nil
}
