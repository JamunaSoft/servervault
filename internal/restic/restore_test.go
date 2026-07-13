package restic

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRepository_Restore_ArgvAndEnv(t *testing.T) {
	runner := &fakeRunner{stdout: `{"message_type":"summary","files_restored":3,"bytes_restored":4096}` + "\n"}
	repo := New(runner, testConfig())

	summary, err := repo.Restore(context.Background(), RestoreOptions{
		SnapshotID: "abc123",
		Target:     "/var/restore/servervault/staging-1",
		Include:    "/var/backups/servervault/postgresql",
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if summary.FilesRestored != 3 || summary.BytesRestored != 4096 {
		t.Errorf("summary = %+v, want FilesRestored=3 BytesRestored=4096", summary)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(runner.calls))
	}
	call := runner.calls[0]
	if call.Name != "restic" {
		t.Errorf("Name = %q, want restic", call.Name)
	}
	wantArgs := []string{"restore", "abc123", "--target", "/var/restore/servervault/staging-1", "--json", "--include", "/var/backups/servervault/postgresql"}
	if strings.Join(call.Args, " ") != strings.Join(wantArgs, " ") {
		t.Errorf("Args = %v, want %v", call.Args, wantArgs)
	}
	if !containsEnvVar(call.Env, "RESTIC_PASSWORD_FILE=/etc/servervault/restic-password") {
		t.Errorf("Env = %v, missing password file var", call.Env)
	}
}

func TestRepository_Restore_NoInclude(t *testing.T) {
	runner := &fakeRunner{stdout: `{"message_type":"summary","files_restored":1,"bytes_restored":10}` + "\n"}
	repo := New(runner, testConfig())

	_, err := repo.Restore(context.Background(), RestoreOptions{SnapshotID: "latest", Target: "/tmp/x"})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for _, a := range runner.calls[0].Args {
		if a == "--include" {
			t.Errorf("--include should not be present when Include is empty: %v", runner.calls[0].Args)
		}
	}
}

func TestRepository_Restore_RequiresSnapshotIDAndTarget(t *testing.T) {
	repo := New(&fakeRunner{}, testConfig())
	ctx := context.Background()

	if _, err := repo.Restore(ctx, RestoreOptions{Target: "/tmp/x"}); err == nil {
		t.Error("Restore with empty SnapshotID should fail")
	}
	if _, err := repo.Restore(ctx, RestoreOptions{SnapshotID: "abc"}); err == nil {
		t.Error("Restore with empty Target should fail")
	}
}

func TestRepository_Restore_WrongPassword_Classified(t *testing.T) {
	// A representative stderr fragment matching restic's documented
	// wrong-password error text, reusing the same classifyStderr
	// signatures already proven against real captured CI output for
	// Backup/Check (see exitcode.go's doc comment) -- not re-captured
	// specifically for Restore, since the classification is subcommand-
	// independent (it only inspects stderr text).
	runner := &fakeRunner{exitCode: 1, stderr: "Fatal: wrong password or no key found\n"}
	repo := New(runner, testConfig())

	_, err := repo.Restore(context.Background(), RestoreOptions{SnapshotID: "abc", Target: "/tmp/x"})
	if err == nil {
		t.Fatal("expected an error")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %v, want *ExitError", err)
	}
	if exitErr.Code != ExitWrongPassword {
		t.Errorf("Code = %s, want %s", exitErr.Code, ExitWrongPassword)
	}
}

func TestRepository_Restore_RepositoryNotFound_Classified(t *testing.T) {
	runner := &fakeRunner{exitCode: 1, stderr: "Fatal: unable to open config file: stat config: no such file or directory\nIs there a repository at the following location?\n"}
	repo := New(runner, testConfig())

	_, err := repo.Restore(context.Background(), RestoreOptions{SnapshotID: "abc", Target: "/tmp/x"})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %v, want *ExitError", err)
	}
	if exitErr.Code != ExitRepositoryNotFound {
		t.Errorf("Code = %s, want %s", exitErr.Code, ExitRepositoryNotFound)
	}
}

func TestRepository_Restore_NoSummaryLine_ReturnsZeroValueNotError(t *testing.T) {
	runner := &fakeRunner{stdout: `{"message_type":"status","percent_done":0.5}` + "\n"}
	repo := New(runner, testConfig())

	summary, err := repo.Restore(context.Background(), RestoreOptions{SnapshotID: "abc", Target: "/tmp/x"})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if summary != (RestoreSummary{}) {
		t.Errorf("summary = %+v, want zero value when no summary line is present", summary)
	}
}

func TestParseRestoreJSON_PrefersLargerOfAlternateFieldNames(t *testing.T) {
	// Different restic versions have used files_restored/total_files and
	// bytes_restored/total_bytes for overlapping concepts; parsing must
	// never under-report by picking whichever field happens to be zero.
	output := []byte(`{"message_type":"summary","files_restored":0,"total_files":5,"bytes_restored":1000,"total_bytes":0}` + "\n")
	summary, found, err := parseRestoreJSON(output)
	if err != nil {
		t.Fatalf("parseRestoreJSON: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if summary.FilesRestored != 5 {
		t.Errorf("FilesRestored = %d, want 5", summary.FilesRestored)
	}
	if summary.BytesRestored != 1000 {
		t.Errorf("BytesRestored = %d, want 1000", summary.BytesRestored)
	}
}

func TestRepository_Stats_ArgvAndParsing(t *testing.T) {
	runner := &fakeRunner{stdout: `{"total_size":123456,"total_file_count":42}`}
	repo := New(runner, testConfig())

	stats, err := repo.Stats(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalSize != 123456 || stats.TotalFileCount != 42 {
		t.Errorf("stats = %+v, want TotalSize=123456 TotalFileCount=42", stats)
	}

	wantArgs := []string{"stats", "--json", "--mode=restore-size", "abc123"}
	if strings.Join(runner.calls[0].Args, " ") != strings.Join(wantArgs, " ") {
		t.Errorf("Args = %v, want %v", runner.calls[0].Args, wantArgs)
	}
}

func TestRepository_Stats_RequiresSnapshotID(t *testing.T) {
	repo := New(&fakeRunner{}, testConfig())
	if _, err := repo.Stats(context.Background(), ""); err == nil {
		t.Error("Stats with empty snapshot ID should fail")
	}
}

func TestRepository_List_ArgvAndParsing(t *testing.T) {
	stdout := `{"message_type":"snapshot","id":"abc123"}` + "\n" +
		`{"name":"dump.zst","path":"/var/backups/servervault/postgresql/app_2026.dump.zst","type":"file","size":2048}` + "\n" +
		`{"name":"postgresql","path":"/var/backups/servervault/postgresql","type":"dir"}` + "\n"
	runner := &fakeRunner{stdout: stdout}
	repo := New(runner, testConfig())

	files, err := repo.List(context.Background(), "abc123", "/var/backups/servervault/postgresql")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("List returned %d entries, want 2 (the header line must be skipped)", len(files))
	}
	if files[0].Path != "/var/backups/servervault/postgresql/app_2026.dump.zst" || files[0].Type != "file" || files[0].Size != 2048 {
		t.Errorf("files[0] = %+v", files[0])
	}
	if files[1].Type != "dir" {
		t.Errorf("files[1].Type = %q, want dir", files[1].Type)
	}

	wantArgs := []string{"ls", "--json", "abc123", "/var/backups/servervault/postgresql"}
	if strings.Join(runner.calls[0].Args, " ") != strings.Join(wantArgs, " ") {
		t.Errorf("Args = %v, want %v", runner.calls[0].Args, wantArgs)
	}
}

func TestRepository_List_NoPath(t *testing.T) {
	runner := &fakeRunner{stdout: `{"message_type":"snapshot"}` + "\n"}
	repo := New(runner, testConfig())

	if _, err := repo.List(context.Background(), "abc123", ""); err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, a := range runner.calls[0].Args {
		if a == "/var/backups/servervault/postgresql" {
			t.Error("unexpected path argument when path is empty")
		}
	}
	if len(runner.calls[0].Args) != 3 { // "ls" "--json" "abc123"
		t.Errorf("Args = %v, want exactly 3 elements", runner.calls[0].Args)
	}
}

func TestRepository_List_RequiresSnapshotID(t *testing.T) {
	repo := New(&fakeRunner{}, testConfig())
	if _, err := repo.List(context.Background(), "", ""); err == nil {
		t.Error("List with empty snapshot ID should fail")
	}
}

func containsEnvVar(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}
