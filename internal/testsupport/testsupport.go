//go:build integration

// Package testsupport provides shared setup/teardown helpers for
// ServerVault's integration test suite (see docs/testing.md). It exists
// only under the `integration` build tag, is never imported by production
// code, and is never part of a default `go test ./...` run.
//
// Safety: every Restic repository this package creates lives under
// t.TempDir() with a randomly generated, test-only password. Every
// PostgreSQL database this package creates is named
// servervault_test_<random>, and the cleanup helper refuses to drop
// anything that doesn't match that prefix, even if called incorrectly.
// Nothing here ever reads a real servervault.yaml or touches a
// production repository, password file, or database.
package testsupport

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JamunaSoft/servervault/internal/config"
)

// testDatabasePrefix is the only prefix NewPostgresDatabase's generated
// names ever use, and the only prefix its cleanup helper will ever drop.
const testDatabasePrefix = "servervault_test_"

// RequireRestic skips the calling test if the restic binary isn't on
// PATH.
func RequireRestic(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic not found on PATH; skipping (see docs/testing.md for installation)")
	}
}

// RequirePostgresBinaries skips the calling test if pg_dump, pg_restore,
// or psql aren't on PATH. It does not require a reachable server or
// database -- see NewPostgresDatabase for that.
func RequirePostgresBinaries(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"pg_dump", "pg_restore", "psql"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not found on PATH; skipping (see docs/testing.md for installation)", bin)
		}
	}
}

// TestPostgresUser returns the OS/database role integration tests should
// authenticate as. SERVERVAULT_TEST_POSTGRES_USER overrides it -- CI uses
// a dedicated low-privilege role created for this purpose; local
// development defaults to "postgres".
func TestPostgresUser() string {
	if u := os.Getenv("SERVERVAULT_TEST_POSTGRES_USER"); u != "" {
		return u
	}
	return "postgres"
}

// NewResticRepository initializes a fresh local Restic repository under
// t.TempDir() and returns a config.ResticConfig pointing at it. It shells
// out to the real restic binary directly (not through internal/restic)
// purely for one-time fixture setup -- internal/restic.Repository
// deliberately has no Init method in production code, and this package
// has no reason to add one just for test convenience.
//
// RequireRestic is called internally, so callers don't need to call it
// separately first.
func NewResticRepository(t *testing.T) config.ResticConfig {
	t.Helper()
	RequireRestic(t)

	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	passwordFile := filepath.Join(dir, "password")

	if err := os.WriteFile(passwordFile, []byte(randomHex(t, 16)), 0o600); err != nil {
		t.Fatalf("testsupport: write password file: %v", err)
	}

	cfg := config.ResticConfig{
		Repository:   "local:" + repoDir,
		PasswordFile: passwordFile,
	}

	cmd := exec.Command("restic", "init")
	cmd.Env = append(os.Environ(),
		"RESTIC_REPOSITORY="+cfg.Repository,
		"RESTIC_PASSWORD_FILE="+cfg.PasswordFile,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("testsupport: restic init: %v\n%s", err, out)
	}

	return cfg
}

// NewPostgresDatabase creates a disposable database named
// servervault_test_<random>, populates it with one table and one row (so
// pg_dump has real content to work with), and registers a t.Cleanup to
// drop it. It skips (not fails) the calling test if the database can't be
// created -- e.g. no privilege in this environment -- rather than trying
// to fall back to any pre-existing database.
//
// RequirePostgresBinaries is called internally.
func NewPostgresDatabase(t *testing.T) config.PostgresConfig {
	t.Helper()
	RequirePostgresBinaries(t)

	dbUser := TestPostgresUser()
	dbName := testDatabasePrefix + randomHex(t, 8)
	requireTestPrefix(t, dbName)

	if err := runAs(dbUser, "createdb", dbName); err != nil {
		t.Skipf("cannot create a test database as %q; skipping (see docs/testing.md): %v", dbUser, err)
	}
	t.Cleanup(func() {
		// Re-checked here, not just trusted from creation time: this
		// function must never drop anything outside its own prefix,
		// even if called by mistake with an unexpected name.
		if !strings.HasPrefix(dbName, testDatabasePrefix) {
			return
		}
		_ = runAs(dbUser, "dropdb", "--if-exists", dbName)
	})

	if err := runAs(dbUser, "psql", "-d", dbName, "-c",
		"CREATE TABLE servervault_probe (id serial primary key, note text); "+
			"INSERT INTO servervault_probe (note) VALUES ('servervault integration test');"); err != nil {
		t.Fatalf("testsupport: populate test database: %v", err)
	}

	return config.PostgresConfig{
		Enabled:   true,
		Database:  dbName,
		User:      dbUser,
		ZstdLevel: 10,
	}
}

func requireTestPrefix(t *testing.T, name string) {
	t.Helper()
	if !strings.HasPrefix(name, testDatabasePrefix) {
		t.Fatalf("testsupport: internal error: generated database name %q is missing the required %q prefix", name, testDatabasePrefix)
	}
}

// runAs runs name as dbUser, via sudo -n -u <dbUser> when the current
// process isn't already running as that user (matching
// internal/postgres's own peer-auth model), and returns a combined
// stdout+stderr on failure for diagnostics.
func runAs(dbUser, name string, args ...string) error {
	cmd := commandAs(dbUser, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w (output: %s)", name, err, bytes.TrimSpace(out))
	}
	return nil
}

func commandAs(dbUser, name string, args ...string) *exec.Cmd {
	if current, err := user.Current(); err == nil && current.Username == dbUser {
		return exec.Command(name, args...)
	}
	sudoArgs := append([]string{"-n", "-u", dbUser, name}, args...)
	return exec.Command("sudo", sudoArgs...)
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("testsupport: generate random suffix: %v", err)
	}
	return hex.EncodeToString(b)
}
