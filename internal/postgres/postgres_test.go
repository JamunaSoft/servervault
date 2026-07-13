package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	osuser "os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
)

// fakeRunner is an execx.Runner test double, keyed by command name, so
// internal/postgres is testable without real pg_dump/pg_restore/psql/zstd/
// sudo binaries. It always drains Stdin (if given) before writing Stdout,
// which matters here specifically: Dump() wires two fake invocations
// together through a real io.Pipe, and an unread pipe write blocks
// forever, so a fake that ignored Stdin would deadlock the test.
type fakeRunner struct {
	mu        sync.Mutex
	calls     []recordedCall
	responses map[string]fakeResponse
}

type recordedCall struct {
	Name string
	Args []string
}

type fakeResponse struct {
	stdout string
	err    error
	// fn, if set, runs synchronously during Run, after stdout is written
	// but before returning -- for tests that need to observe filesystem
	// state that's only visible during the call, e.g. handoff file
	// permissions before RestoreToTemp's own cleanup removes them.
	fn func(opts execx.RunOptions)
}

func (f *fakeRunner) Run(ctx context.Context, opts execx.RunOptions) error {
	f.mu.Lock()
	f.calls = append(f.calls, recordedCall{Name: opts.Name, Args: append([]string{}, opts.Args...)})
	resp := f.responses[opts.Name]
	f.mu.Unlock()

	if opts.Stdin != nil {
		_, _ = io.Copy(io.Discard, opts.Stdin)
	}
	if opts.Stdout != nil && resp.stdout != "" {
		_, _ = io.WriteString(opts.Stdout, resp.stdout)
	}
	if resp.fn != nil {
		resp.fn(opts)
	}
	return resp.err
}

func (f *fakeRunner) callsFor(name string) []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recordedCall
	for _, c := range f.calls {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

func testConfig() config.PostgresConfig {
	return config.PostgresConfig{
		Enabled:   true,
		Database:  "app_production",
		User:      "", // "" => needsSudo is false, argv assertions stay simple
		ZstdLevel: 10,
	}
}

func TestNeedsSudo(t *testing.T) {
	current, err := currentUsername(t)
	if err != nil {
		t.Skipf("cannot determine current user: %v", err)
	}

	tests := []struct {
		name       string
		targetUser string
		want       bool
	}{
		{name: "empty user", targetUser: "", want: false},
		{name: "same as current user", targetUser: current, want: false},
		{name: "different user", targetUser: "definitely-not-" + current, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsSudo(tt.targetUser); got != tt.want {
				t.Errorf("needsSudo(%q) = %v, want %v", tt.targetUser, got, tt.want)
			}
		})
	}
}

func TestCommandFor_NoSudoNeeded(t *testing.T) {
	c := New(&fakeRunner{}, config.PostgresConfig{User: ""})
	name, args := c.commandFor("psql", []string{"-c", "SELECT 1"})
	if name != "psql" {
		t.Errorf("name = %q, want %q", name, "psql")
	}
	if len(args) != 2 || args[0] != "-c" {
		t.Errorf("args = %v, want unchanged", args)
	}
}

func TestCommandFor_WrapsWithSudo(t *testing.T) {
	current, err := currentUsername(t)
	if err != nil {
		t.Skipf("cannot determine current user: %v", err)
	}
	c := New(&fakeRunner{}, config.PostgresConfig{User: "definitely-not-" + current})
	name, args := c.commandFor("pg_dump", []string{"mydb"})

	if name != "sudo" {
		t.Fatalf("name = %q, want %q", name, "sudo")
	}
	want := []string{"-n", "-u", "definitely-not-" + current, "pg_dump", "mydb"}
	if !equalArgs(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestClient_BaseArgs_EmptyHostOmitsFlags(t *testing.T) {
	c := New(&fakeRunner{}, config.PostgresConfig{Host: ""})
	if args := c.baseArgs(); args != nil {
		t.Errorf("baseArgs() with empty Host = %v, want nil (Unix socket + peer auth)", args)
	}
}

func TestClient_BaseArgs_HostSetIncludesFlags(t *testing.T) {
	c := New(&fakeRunner{}, config.PostgresConfig{Host: "10.0.0.5", Port: 5433})
	want := []string{"-h", "10.0.0.5", "-p", "5433"}
	if got := c.baseArgs(); !equalArgs(got, want) {
		t.Errorf("baseArgs() = %v, want %v", got, want)
	}
}

func TestPing_Success(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{"psql": {stdout: "1\n"}}}
	c := New(runner, testConfig())

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping(): unexpected error: %v", err)
	}
	calls := runner.callsFor("psql")
	if len(calls) != 1 {
		t.Fatalf("psql calls = %d, want 1", len(calls))
	}
	want := []string{"-d", "app_production", "-Atc", "SELECT 1"}
	if !equalArgs(calls[0].Args, want) {
		t.Errorf("args = %v, want %v", calls[0].Args, want)
	}
}

func TestPing_UnexpectedResponse(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{"psql": {stdout: "0\n"}}}
	c := New(runner, testConfig())

	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping() with an unexpected response: want an error, got nil")
	}
}

func TestPing_CommandFailure(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{"psql": {err: fmt.Errorf("connection refused")}}}
	c := New(runner, testConfig())

	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping() with a command failure: want an error, got nil")
	}
}

func TestDump_CreatesFileWithRestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"pg_dump": {stdout: "fake pg_dump output"},
		"zstd":    {stdout: "fake compressed bytes"},
	}}
	c := New(runner, testConfig())

	meta, err := c.Dump(context.Background(), dir)
	if err != nil {
		t.Fatalf("Dump(): unexpected error: %v", err)
	}

	info, err := os.Stat(meta.Path)
	if err != nil {
		t.Fatalf("stat dump file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("dump file mode = %s, want 0600", perm)
	}
	if !strings.HasPrefix(meta.Path, dir) {
		t.Errorf("dump file %q not created inside %q", meta.Path, dir)
	}
	if meta.Bytes == 0 {
		t.Error("Metadata.Bytes = 0, want > 0 (zstd's fake output should have been written)")
	}
}

func TestDump_ArgvConstruction(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"pg_dump": {stdout: "x"}, "zstd": {stdout: "y"},
	}}
	c := New(runner, testConfig())

	if _, err := c.Dump(context.Background(), dir); err != nil {
		t.Fatalf("Dump(): unexpected error: %v", err)
	}

	dumpCalls := runner.callsFor("pg_dump")
	if len(dumpCalls) != 1 {
		t.Fatalf("pg_dump calls = %d, want 1", len(dumpCalls))
	}
	want := []string{"--format=custom", "--no-owner", "--no-privileges", "app_production"}
	if !equalArgs(dumpCalls[0].Args, want) {
		t.Errorf("pg_dump args = %v, want %v", dumpCalls[0].Args, want)
	}

	zstdCalls := runner.callsFor("zstd")
	if len(zstdCalls) != 1 {
		t.Fatalf("zstd calls = %d, want 1", len(zstdCalls))
	}
	wantZstd := []string{"-T0", "-10"}
	if !equalArgs(zstdCalls[0].Args, wantZstd) {
		t.Errorf("zstd args = %v, want %v", zstdCalls[0].Args, wantZstd)
	}
}

func TestDump_PgDumpFailure(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"pg_dump": {err: fmt.Errorf("connection refused")},
		"zstd":    {stdout: "irrelevant"},
	}}
	c := New(runner, testConfig())

	meta, err := c.Dump(context.Background(), dir)
	if err == nil {
		t.Fatal("Dump() with a pg_dump failure: want an error, got nil")
	}
	var dumpErr *DumpError
	if !errors.As(err, &dumpErr) || dumpErr.Stage != "pg_dump" {
		t.Errorf("error = %v, want DumpError{Stage: \"pg_dump\"}", err)
	}
	// The (possibly partial) destination path is still reported so the
	// caller can clean it up.
	if meta.Path == "" {
		t.Error("Metadata.Path is empty on failure; caller has nothing to remove")
	}
}

func TestDump_ZstdFailure(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"pg_dump": {stdout: "some data"},
		"zstd":    {err: fmt.Errorf("compression failed")},
	}}
	c := New(runner, testConfig())

	_, err := c.Dump(context.Background(), dir)
	if err == nil {
		t.Fatal("Dump() with a zstd failure: want an error, got nil")
	}
	var dumpErr *DumpError
	if !errors.As(err, &dumpErr) || dumpErr.Stage != "zstd" {
		t.Errorf("error = %v, want DumpError{Stage: \"zstd\"}", err)
	}
}

func TestVerifyDump_Success(t *testing.T) {
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "fake.dump.zst")
	if err := os.WriteFile(dumpPath, []byte("compressed"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"zstd":       {stdout: "decompressed content"},
		"pg_restore": {stdout: "table of contents"},
	}}
	c := New(runner, testConfig())

	if err := c.VerifyDump(context.Background(), dumpPath); err != nil {
		t.Fatalf("VerifyDump(): unexpected error: %v", err)
	}

	remaining, _ := filepath.Glob(filepath.Join(dir, "verify-*"))
	if len(remaining) != 0 {
		t.Errorf("temporary decompressed file(s) left behind: %v", remaining)
	}
}

func TestVerifyDump_CleansUpOnPgRestoreFailure(t *testing.T) {
	// This is the exact gap the shell implementation has under `set -e`
	// (see verify.go's doc comment): a failed pg_restore --list must not
	// leak the decompressed temp file.
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "fake.dump.zst")
	if err := os.WriteFile(dumpPath, []byte("compressed"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"zstd":       {stdout: "decompressed content"},
		"pg_restore": {err: fmt.Errorf("corrupt archive")},
	}}
	c := New(runner, testConfig())

	err := c.VerifyDump(context.Background(), dumpPath)
	if err == nil {
		t.Fatal("VerifyDump() with a pg_restore failure: want an error, got nil")
	}
	var verifyErr *VerifyError
	if !errors.As(err, &verifyErr) || verifyErr.Stage != "pg_restore" {
		t.Errorf("error = %v, want VerifyError{Stage: \"pg_restore\"}", err)
	}

	remaining, _ := filepath.Glob(filepath.Join(dir, "verify-*"))
	if len(remaining) != 0 {
		t.Errorf("temporary decompressed file(s) left behind after failure: %v", remaining)
	}
}

func TestVerifyDump_CleansUpOnDecompressFailure(t *testing.T) {
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "fake.dump.zst")
	if err := os.WriteFile(dumpPath, []byte("not really compressed"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	runner := &fakeRunner{responses: map[string]fakeResponse{
		"zstd": {err: fmt.Errorf("not a zstd frame")},
	}}
	c := New(runner, testConfig())

	err := c.VerifyDump(context.Background(), dumpPath)
	if err == nil {
		t.Fatal("VerifyDump() with a decompress failure: want an error, got nil")
	}

	remaining, _ := filepath.Glob(filepath.Join(dir, "verify-*"))
	if len(remaining) != 0 {
		t.Errorf("temporary decompressed file(s) left behind after failure: %v", remaining)
	}
}

func TestDumpFilePattern_SanitizesDatabaseName(t *testing.T) {
	pattern := dumpFilePattern("app/../production; rm -rf")
	for _, bad := range []string{"/", ";", " "} {
		if strings.Contains(pattern, bad) {
			t.Errorf("dumpFilePattern() = %q, contains unsafe character %q", pattern, bad)
		}
	}
}

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func currentUsername(t *testing.T) (string, error) {
	t.Helper()
	u, err := osuser.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}
