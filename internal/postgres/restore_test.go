package postgres

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JamunaSoft/servervault/internal/execx"
)

func TestValidDatabaseName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"servervault_restore_abc123", true},
		{"app.production-2026", true},
		{"", false},
		{"app; DROP TABLE users;--", false},
		{"app'name", false},
		{"app name", false},
		{"app\"name", false},
	}
	for _, tt := range tests {
		if got := validDatabaseName(tt.name); got != tt.want {
			t.Errorf("validDatabaseName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestClient_DatabaseExists(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		want   bool
	}{
		{"exists", "1\n", true},
		{"does not exist", "\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{responses: map[string]fakeResponse{
				"psql": {stdout: tt.stdout},
			}}
			c := New(runner, testConfig())

			got, err := c.DatabaseExists(context.Background(), "servervault_restore_abc")
			if err != nil {
				t.Fatalf("DatabaseExists: %v", err)
			}
			if got != tt.want {
				t.Errorf("DatabaseExists = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClient_DatabaseExists_RejectsUnsafeName(t *testing.T) {
	c := New(&fakeRunner{}, testConfig())
	_, err := c.DatabaseExists(context.Background(), "app'; DROP TABLE users;--")
	if err == nil {
		t.Fatal("DatabaseExists with an unsafe name should fail before running anything")
	}
}

func TestClient_CreateDatabase_RefusesIfAlreadyExists(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"psql": {stdout: "1\n"}, // exists-check reports the DB already exists
	}}
	c := New(runner, testConfig())

	err := c.CreateDatabase(context.Background(), "servervault_restore_abc")
	if err == nil {
		t.Fatal("CreateDatabase should refuse to proceed when the database already exists")
	}
	if len(runner.callsFor("createdb")) != 0 {
		t.Error("createdb must not be invoked when the pre-check found an existing database")
	}
}

func TestClient_CreateDatabase_Success(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"psql":     {stdout: "\n"}, // exists-check: does not exist
		"createdb": {},
	}}
	c := New(runner, testConfig())

	if err := c.CreateDatabase(context.Background(), "servervault_restore_abc"); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}

	calls := runner.callsFor("createdb")
	if len(calls) != 1 {
		t.Fatalf("expected 1 createdb call, got %d", len(calls))
	}
	if calls[0].Args[len(calls[0].Args)-1] != "servervault_restore_abc" {
		t.Errorf("createdb args = %v, want last arg to be the database name", calls[0].Args)
	}
}

func TestClient_CreateDatabase_RejectsUnsafeName(t *testing.T) {
	c := New(&fakeRunner{}, testConfig())
	if err := c.CreateDatabase(context.Background(), "app; DROP TABLE users;"); err == nil {
		t.Fatal("CreateDatabase with an unsafe name should fail")
	}
}

func TestClient_DropDatabase(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{"dropdb": {}}}
	c := New(runner, testConfig())

	if err := c.DropDatabase(context.Background(), "servervault_restore_abc"); err != nil {
		t.Fatalf("DropDatabase: %v", err)
	}
	calls := runner.callsFor("dropdb")
	if len(calls) != 1 {
		t.Fatalf("expected 1 dropdb call, got %d", len(calls))
	}
	foundIfExists := false
	for _, a := range calls[0].Args {
		if a == "--if-exists" {
			foundIfExists = true
		}
	}
	if !foundIfExists {
		t.Errorf("dropdb args = %v, want --if-exists present", calls[0].Args)
	}
}

func TestClient_DropDatabase_RejectsUnsafeName(t *testing.T) {
	c := New(&fakeRunner{}, testConfig())
	if err := c.DropDatabase(context.Background(), ""); err == nil {
		t.Fatal("DropDatabase with an empty name should fail")
	}
}

func TestClient_RestoreToTemp_Success(t *testing.T) {
	dir := t.TempDir()
	dumpPath := writeTempFile(t, dir, "fake-compressed-dump-content")

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"zstd":       {stdout: "decompressed dump bytes"},
		"pg_restore": {},
	}}
	c := New(runner, testConfig())

	err := c.RestoreToTemp(context.Background(), dumpPath, "servervault_restore_abc")
	if err != nil {
		t.Fatalf("RestoreToTemp: %v", err)
	}

	restoreCalls := runner.callsFor("pg_restore")
	if len(restoreCalls) != 2 {
		t.Fatalf("expected 2 pg_restore calls (validate --list, then real restore), got %d", len(restoreCalls))
	}
	// First call: --list validation, no --dbname.
	for _, a := range restoreCalls[0].Args {
		if strings.HasPrefix(a, "--dbname=") {
			t.Errorf("validation pg_restore call should not include --dbname: %v", restoreCalls[0].Args)
		}
	}
	// Second call: the real restore, targeting exactly the temp database.
	foundDBName := false
	for _, a := range restoreCalls[1].Args {
		if a == "--dbname=servervault_restore_abc" {
			foundDBName = true
		}
	}
	if !foundDBName {
		t.Errorf("restore pg_restore call args = %v, want --dbname=servervault_restore_abc", restoreCalls[1].Args)
	}
}

func TestClient_RestoreToTemp_RejectsUnsafeName(t *testing.T) {
	dir := t.TempDir()
	dumpPath := writeTempFile(t, dir, "content")
	c := New(&fakeRunner{}, testConfig())

	err := c.RestoreToTemp(context.Background(), dumpPath, "app'; DROP TABLE users;--")
	if err == nil {
		t.Fatal("RestoreToTemp with an unsafe database name should fail")
	}
}

func TestClient_RestoreToTemp_ValidationFailureNeverRestores(t *testing.T) {
	dir := t.TempDir()
	dumpPath := writeTempFile(t, dir, "corrupted content")

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"zstd":       {stdout: "garbage"},
		"pg_restore": {err: errors.New("pg_restore: input file appears to be a text format dump")},
	}}
	c := New(runner, testConfig())

	err := c.RestoreToTemp(context.Background(), dumpPath, "servervault_restore_abc")
	if err == nil {
		t.Fatal("RestoreToTemp should fail when validation (--list) fails")
	}
	var restoreErr *RestoreError
	if !errors.As(err, &restoreErr) || restoreErr.Stage != "validate" {
		t.Errorf("error = %v, want RestoreError with Stage=validate", err)
	}
	// Only the validation call should have happened -- the fake fails
	// every pg_restore invocation identically, so if a second (real
	// restore) call had been made we'd still only see one error, but we
	// can at least confirm exactly one pg_restore call was attempted
	// before RestoreToTemp gave up.
	if len(runner.callsFor("pg_restore")) != 1 {
		t.Errorf("expected exactly 1 pg_restore call before bailing out on validation failure, got %d", len(runner.callsFor("pg_restore")))
	}
}

// TestClient_RestoreToTemp_HandoffPermissions pins the fix for
// TestIntegration_Restore_TempDB_Success failing with "permission
// denied": the sudo'd pg_restore call needs to read the decompressed
// dump as a different OS user, so the handoff directory/file it lives in
// must be widened -- but only when that privilege separation actually
// applies, and never by touching dumpPath's own (deliberately
// owner-only, "unreadable parent directory") directory.
func TestClient_RestoreToTemp_HandoffPermissions(t *testing.T) {
	current, err := currentUsername(t)
	if err != nil {
		t.Skipf("cannot determine current user: %v", err)
	}

	tests := []struct {
		name         string
		user         string
		wantDirMode  os.FileMode
		wantFileMode os.FileMode
	}{
		{name: "same user as current process -- no widening", user: "", wantDirMode: 0o700, wantFileMode: 0o600},
		{name: "different user -- widened for sudo'd pg_restore", user: "definitely-not-" + current, wantDirMode: handoffDirMode, wantFileMode: handoffFileMode},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// dumpPath's own directory: owner-only, like a production
			// restic-extraction directory or a Go t.TempDir() -- and it
			// must stay that way, since RestoreToTemp never asks the
			// configured PostgreSQL OS user to read anything from it
			// directly (only the current process ever decompresses
			// dumpPath; see the handoffDir doc comment).
			dir := t.TempDir()
			if err := os.Chmod(dir, 0o700); err != nil {
				t.Fatalf("chmod fixture dir: %v", err)
			}
			dumpPath := writeTempFile(t, dir, "fake-compressed-dump-content")

			var handoffDir string
			var gotDirMode, gotFileMode os.FileMode
			runner := &fakeRunner{responses: map[string]fakeResponse{
				"zstd": {stdout: "decompressed dump bytes", fn: func(opts execx.RunOptions) {
					f, ok := opts.Stdout.(*os.File)
					if !ok {
						t.Fatalf("zstd stdout is not a *os.File: %T", opts.Stdout)
					}
					handoffDir = filepath.Dir(f.Name())
					dirInfo, statErr := os.Stat(handoffDir)
					if statErr != nil {
						t.Fatalf("stat handoff dir: %v", statErr)
					}
					gotDirMode = dirInfo.Mode().Perm()
					fileInfo, statErr := os.Stat(f.Name())
					if statErr != nil {
						t.Fatalf("stat handoff file: %v", statErr)
					}
					gotFileMode = fileInfo.Mode().Perm()
				}},
				"pg_restore": {},
			}}
			cfg := testConfig()
			cfg.User = tt.user
			c := New(runner, cfg)

			if err := c.RestoreToTemp(context.Background(), dumpPath, "servervault_restore_abc"); err != nil {
				t.Fatalf("RestoreToTemp: %v", err)
			}

			if gotDirMode != tt.wantDirMode {
				t.Errorf("handoff directory mode = %s, want %s", gotDirMode, tt.wantDirMode)
			}
			if gotFileMode != tt.wantFileMode {
				t.Errorf("handoff file mode = %s, want %s", gotFileMode, tt.wantFileMode)
			}

			// No permission widening outside the dedicated handoff
			// directory.
			dumpDirInfo, statErr := os.Stat(dir)
			if statErr != nil {
				t.Fatalf("stat dump dir: %v", statErr)
			}
			if perm := dumpDirInfo.Mode().Perm(); perm != 0o700 {
				t.Errorf("dumpPath's own directory mode changed to %s, want unchanged 0700 -- RestoreToTemp must never widen a caller-supplied directory", perm)
			}

			// Cleanup after restore: the handoff directory must not
			// outlive the call.
			if handoffDir == "" {
				t.Fatal("zstd fake was never invoked; test setup is broken")
			}
			if _, statErr := os.Stat(handoffDir); !os.IsNotExist(statErr) {
				t.Errorf("handoff directory %q still exists after RestoreToTemp returned, want it removed", handoffDir)
			}
		})
	}
}

// TestClient_RestoreToTemp_CleansUpHandoffDirOnEveryPath complements
// TestClient_RestoreToTemp_CleansUpTempFileOnEveryPath (which covers
// dumpPath's own directory): it checks the *new*, dedicated handoff
// directory under os.TempDir() is also never leaked, including on a
// restore failure.
func TestClient_RestoreToTemp_CleansUpHandoffDirOnEveryPath(t *testing.T) {
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), pgHandoffDirPrefix+"*"))

	dir := t.TempDir()
	dumpPath := writeTempFile(t, dir, "content")

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"zstd":       {stdout: "x"},
		"pg_restore": {err: errors.New("boom")},
	}}
	c := New(runner, testConfig())
	_ = c.RestoreToTemp(context.Background(), dumpPath, "servervault_restore_abc")

	after, _ := filepath.Glob(filepath.Join(os.TempDir(), pgHandoffDirPrefix+"*"))
	if len(after) != len(before) {
		t.Errorf("handoff directory leaked after a failed RestoreToTemp: %d before, %d after (%v)", len(before), len(after), after)
	}
}

func TestClient_RestoreToTemp_CleansUpTempFileOnEveryPath(t *testing.T) {
	dir := t.TempDir()
	dumpPath := writeTempFile(t, dir, "content")

	countBefore := countFiles(t, dir)

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"zstd":       {stdout: "x"},
		"pg_restore": {err: errors.New("boom")},
	}}
	c := New(runner, testConfig())
	_ = c.RestoreToTemp(context.Background(), dumpPath, "servervault_restore_abc")

	countAfter := countFiles(t, dir)
	if countAfter != countBefore {
		t.Errorf("temp file leaked: %d files before, %d after a failed RestoreToTemp", countBefore, countAfter)
	}
}

func TestClient_PingDatabase_Success(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{"psql": {stdout: "1\n"}}}
	c := New(runner, testConfig())

	if err := c.PingDatabase(context.Background(), "servervault_restore_abc"); err != nil {
		t.Fatalf("PingDatabase: %v", err)
	}
}

func TestClient_PingDatabase_UnexpectedResponse(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{"psql": {stdout: "boom\n"}}}
	c := New(runner, testConfig())

	if err := c.PingDatabase(context.Background(), "servervault_restore_abc"); err == nil {
		t.Fatal("PingDatabase with an unexpected response should fail")
	}
}

func TestClient_PingDatabase_RejectsUnsafeName(t *testing.T) {
	c := New(&fakeRunner{}, testConfig())
	if err := c.PingDatabase(context.Background(), ""); err == nil {
		t.Fatal("PingDatabase with an empty name should fail")
	}
}

func writeTempFile(t *testing.T, dir, content string) string {
	t.Helper()
	f, err := os.CreateTemp(dir, "dump-*.dump.zst")
	if err != nil {
		t.Fatalf("create fixture file: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	return f.Name()
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	return len(entries)
}
