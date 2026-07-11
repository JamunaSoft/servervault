package restic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
)

// fakeRunner is an execx.Runner test double that records every
// invocation and returns a canned response, so internal/restic is
// testable without the real restic binary.
type fakeRunner struct {
	calls []recordedCall

	stdout   string
	stderr   string
	exitCode int // 0 = success
	runErr   error
}

type recordedCall struct {
	Name string
	Args []string
	Env  []string
}

func (f *fakeRunner) Run(ctx context.Context, opts execx.RunOptions) error {
	f.calls = append(f.calls, recordedCall{Name: opts.Name, Args: append([]string{}, opts.Args...), Env: append([]string{}, opts.Env...)})

	if opts.Stdout != nil {
		_, _ = io.WriteString(opts.Stdout, f.stdout)
	}
	if opts.Stderr != nil {
		_, _ = io.WriteString(opts.Stderr, f.stderr)
	}

	if f.runErr != nil {
		return f.runErr
	}
	if f.exitCode != 0 {
		return &execx.ExitError{Code: f.exitCode, Err: fmt.Errorf("exit status %d", f.exitCode)}
	}
	return nil
}

func testConfig() config.ResticConfig {
	return config.ResticConfig{
		Repository:   "sftp:user@host:/backups/servervault",
		PasswordFile: "/etc/servervault/restic-password",
	}
}

func TestRepository_CatConfig_ArgvAndEnv(t *testing.T) {
	runner := &fakeRunner{}
	repo := New(runner, testConfig())

	if err := repo.CatConfig(context.Background()); err != nil {
		t.Fatalf("CatConfig(): unexpected error: %v", err)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(runner.calls))
	}
	call := runner.calls[0]
	if call.Name != "restic" {
		t.Errorf("Name = %q, want %q", call.Name, "restic")
	}
	wantArgs := []string{"cat", "config"}
	if !equalArgs(call.Args, wantArgs) {
		t.Errorf("Args = %v, want %v", call.Args, wantArgs)
	}
	if !containsEnv(call.Env, "RESTIC_REPOSITORY=sftp:user@host:/backups/servervault") {
		t.Errorf("Env = %v, missing RESTIC_REPOSITORY", call.Env)
	}
	if !containsEnv(call.Env, "RESTIC_PASSWORD_FILE=/etc/servervault/restic-password") {
		t.Errorf("Env = %v, missing RESTIC_PASSWORD_FILE", call.Env)
	}
}

func TestRepository_CatConfig_SFTPCommandEnv(t *testing.T) {
	runner := &fakeRunner{}
	cfg := testConfig()
	cfg.SFTPCommand = "ssh -i /etc/servervault/ssh/backup_key -o IdentitiesOnly=yes"
	repo := New(runner, cfg)

	_ = repo.CatConfig(context.Background())

	if !containsEnv(runner.calls[0].Env, "RESTIC_SFTP_COMMAND="+cfg.SFTPCommand) {
		t.Errorf("Env = %v, missing RESTIC_SFTP_COMMAND", runner.calls[0].Env)
	}
}

func TestRepository_CatConfig_NoSFTPCommandWhenUnset(t *testing.T) {
	runner := &fakeRunner{}
	repo := New(runner, testConfig())
	_ = repo.CatConfig(context.Background())

	for _, e := range runner.calls[0].Env {
		if strings.HasPrefix(e, "RESTIC_SFTP_COMMAND=") {
			t.Errorf("Env unexpectedly contains RESTIC_SFTP_COMMAND: %v", runner.calls[0].Env)
		}
	}
}

func TestRepository_CatConfig_NeverPassesPasswordFileAsArg(t *testing.T) {
	// The password file path must travel via environment only -- never
	// argv, where it would be visible in `ps aux`.
	runner := &fakeRunner{}
	repo := New(runner, testConfig())
	_ = repo.CatConfig(context.Background())

	for _, a := range runner.calls[0].Args {
		if strings.Contains(a, "restic-password") {
			t.Errorf("Args unexpectedly contains the password file path: %v", runner.calls[0].Args)
		}
	}
}

func TestRepository_CatConfig_Failure(t *testing.T) {
	runner := &fakeRunner{exitCode: 12} // wrong password
	repo := New(runner, testConfig())

	err := repo.CatConfig(context.Background())
	if err == nil {
		t.Fatal("CatConfig(): want an error, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %v, want it to unwrap to *ExitError", err)
	}
	if exitErr.Code != ExitWrongPassword {
		t.Errorf("Code = %v, want ExitWrongPassword", exitErr.Code)
	}
}

func TestRepository_Check_ArgvWithReadData(t *testing.T) {
	runner := &fakeRunner{}
	repo := New(runner, testConfig())

	if err := repo.Check(context.Background(), CheckOptions{ReadData: true}); err != nil {
		t.Fatalf("Check(): unexpected error: %v", err)
	}
	want := []string{"check", "--read-data"}
	if !equalArgs(runner.calls[0].Args, want) {
		t.Errorf("Args = %v, want %v", runner.calls[0].Args, want)
	}
}

func TestRepository_Check_ArgvWithoutReadData(t *testing.T) {
	runner := &fakeRunner{}
	repo := New(runner, testConfig())

	_ = repo.Check(context.Background(), CheckOptions{})
	want := []string{"check"}
	if !equalArgs(runner.calls[0].Args, want) {
		t.Errorf("Args = %v, want %v", runner.calls[0].Args, want)
	}
}

func TestRepository_Backup_ArgvConstruction(t *testing.T) {
	runner := &fakeRunner{stdout: `{"message_type":"summary","files_new":2,"files_changed":1,"data_added":1024,"snapshot_id":"abcd1234"}` + "\n"}
	repo := New(runner, testConfig())

	summary, err := repo.Backup(context.Background(), BackupOptions{
		Paths:       []string{"/var/www", "/tmp/dump.zst"},
		ExcludeFile: "/etc/servervault/excludes.txt",
		Tags:        []string{"servervault", "srv-eea-bd"},
		HostTag:     "srv-eea-bd",
	})
	if err != nil {
		t.Fatalf("Backup(): unexpected error: %v", err)
	}

	want := []string{
		"backup", "--json",
		"--host", "srv-eea-bd",
		"--tag", "servervault", "--tag", "srv-eea-bd",
		"--exclude-file", "/etc/servervault/excludes.txt",
		"/var/www", "/tmp/dump.zst",
	}
	if !equalArgs(runner.calls[0].Args, want) {
		t.Errorf("Args = %v, want %v", runner.calls[0].Args, want)
	}

	if summary.SnapshotID != "abcd1234" || summary.FilesNew != 2 || summary.FilesChanged != 1 || summary.BytesAdded != 1024 {
		t.Errorf("Summary = %+v, unexpected values", summary)
	}
}

func TestRepository_Backup_AdversarialPathIsNeverInterpreted(t *testing.T) {
	// A path (or database-derived filename) containing shell metacharacters
	// must appear as one literal argv element, never split or interpreted --
	// there is no shell in the execution path to interpret it in the first
	// place, but this proves the argv construction doesn't accidentally
	// concatenate anything into a string.
	runner := &fakeRunner{stdout: `{"message_type":"summary","snapshot_id":"x"}` + "\n"}
	repo := New(runner, testConfig())

	adversarial := "/var/www; rm -rf / #"
	_, err := repo.Backup(context.Background(), BackupOptions{Paths: []string{adversarial}})
	if err != nil {
		t.Fatalf("Backup(): unexpected error: %v", err)
	}

	args := runner.calls[0].Args
	found := false
	for _, a := range args {
		if a == adversarial {
			found = true
		}
		if strings.Contains(a, "rm") && a != adversarial {
			t.Errorf("adversarial input was split across multiple argv elements: %v", args)
		}
	}
	if !found {
		t.Errorf("adversarial path not found as a single literal argv element: %v", args)
	}
}

func TestRepository_Backup_ExitCode3IsWarningNotError(t *testing.T) {
	runner := &fakeRunner{
		exitCode: 3,
		stdout: `{"message_type":"error","item":"/var/www/socket","error":{"message":"permission denied"}}` + "\n" +
			`{"message_type":"summary","files_new":5,"snapshot_id":"partial123"}` + "\n",
	}
	repo := New(runner, testConfig())

	summary, err := repo.Backup(context.Background(), BackupOptions{Paths: []string{"/var/www"}})
	if err != nil {
		t.Fatalf("Backup() with restic exit code 3: want success (warning, not error), got: %v", err)
	}
	if summary.SnapshotID != "partial123" {
		t.Errorf("SnapshotID = %q, want %q", summary.SnapshotID, "partial123")
	}
	if len(summary.Warnings) != 1 || !strings.Contains(summary.Warnings[0], "permission denied") {
		t.Errorf("Warnings = %v, want one entry mentioning permission denied", summary.Warnings)
	}
}

func TestRepository_Backup_HardFailureExitCode1(t *testing.T) {
	runner := &fakeRunner{exitCode: 1, stderr: "fatal: unable to open repository"}
	repo := New(runner, testConfig())

	_, err := repo.Backup(context.Background(), BackupOptions{Paths: []string{"/var/www"}})
	if err == nil {
		t.Fatal("Backup(): want an error for exit code 1, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitGenericError {
		t.Errorf("error = %v, want ExitError{Code: ExitGenericError}", err)
	}
}

func TestRepository_Backup_MissingSummaryLineIsError(t *testing.T) {
	runner := &fakeRunner{stdout: `{"message_type":"status"}` + "\n"}
	repo := New(runner, testConfig())

	_, err := repo.Backup(context.Background(), BackupOptions{Paths: []string{"/var/www"}})
	if err == nil {
		t.Fatal("Backup() with no summary event: want an error, got nil")
	}
}

func TestRepository_Snapshots_ArgvAndParsing(t *testing.T) {
	runner := &fakeRunner{stdout: `[{"id":"abc123","hostname":"srv-eea-bd","tags":["servervault"],"paths":["/var/www"]}]`}
	repo := New(runner, testConfig())

	snaps, err := repo.Snapshots(context.Background(), SnapshotsOptions{Host: "srv-eea-bd", Tags: []string{"servervault"}, Latest: 5})
	if err != nil {
		t.Fatalf("Snapshots(): unexpected error: %v", err)
	}

	want := []string{"snapshots", "--json", "--host", "srv-eea-bd", "--tag", "servervault", "--latest", "5"}
	if !equalArgs(runner.calls[0].Args, want) {
		t.Errorf("Args = %v, want %v", runner.calls[0].Args, want)
	}
	if len(snaps) != 1 || snaps[0].ID != "abc123" {
		t.Errorf("Snapshots = %+v, unexpected result", snaps)
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

func containsEnv(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}
