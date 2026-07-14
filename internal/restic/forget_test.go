package restic

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRepository_Forget_ArgvAndEnv(t *testing.T) {
	runner := &fakeRunner{stdout: `[{"keep":[{"id":"keep1"}],"remove":[{"id":"remove1"},{"id":"remove2"}]}]`}
	repo := New(runner, testConfig())

	summary, err := repo.Forget(context.Background(), ForgetOptions{
		Host:        "myhost",
		Tags:        []string{"servervault"},
		KeepDaily:   7,
		KeepWeekly:  4,
		KeepMonthly: 12,
		Prune:       true,
	})
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if len(summary.KeptSnapshotIDs) != 1 || summary.KeptSnapshotIDs[0] != "keep1" {
		t.Errorf("KeptSnapshotIDs = %v, want [keep1]", summary.KeptSnapshotIDs)
	}
	if len(summary.RemovedSnapshotIDs) != 2 {
		t.Errorf("RemovedSnapshotIDs = %v, want 2 entries", summary.RemovedSnapshotIDs)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(runner.calls))
	}
	call := runner.calls[0]
	if call.Name != "restic" {
		t.Errorf("Name = %q, want restic", call.Name)
	}
	wantArgs := []string{
		"forget", "--json", "--host", "myhost", "--tag", "servervault",
		"--keep-daily", "7", "--keep-weekly", "4", "--keep-monthly", "12", "--prune",
	}
	if strings.Join(call.Args, " ") != strings.Join(wantArgs, " ") {
		t.Errorf("Args = %v, want %v", call.Args, wantArgs)
	}
	if !containsEnvVar(call.Env, "RESTIC_PASSWORD_FILE=/etc/servervault/restic-password") {
		t.Errorf("Env = %v, missing password file var", call.Env)
	}
}

func TestRepository_Forget_DryRun_AddsFlagAndNeverPrunes(t *testing.T) {
	runner := &fakeRunner{stdout: `[{"keep":[],"remove":[{"id":"a"}]}]`}
	repo := New(runner, testConfig())

	_, err := repo.Forget(context.Background(), ForgetOptions{
		Host: "myhost", KeepDaily: 7, DryRun: true,
	})
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}

	var sawDryRun, sawPrune bool
	for _, a := range runner.calls[0].Args {
		if a == "--dry-run" {
			sawDryRun = true
		}
		if a == "--prune" {
			sawPrune = true
		}
	}
	if !sawDryRun {
		t.Error("expected --dry-run in args")
	}
	if sawPrune {
		t.Error("--prune must not be present when DryRun is set without Prune")
	}
}

func TestRepository_Forget_NoTagsOmitsTagFlags(t *testing.T) {
	runner := &fakeRunner{stdout: `[]`}
	repo := New(runner, testConfig())

	if _, err := repo.Forget(context.Background(), ForgetOptions{Host: "h", KeepDaily: 1}); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	for _, a := range runner.calls[0].Args {
		if a == "--tag" {
			t.Errorf("--tag should not appear when Tags is empty: %v", runner.calls[0].Args)
		}
	}
}

func TestRepository_Forget_WrongPassword_Classified(t *testing.T) {
	runner := &fakeRunner{exitCode: 1, stderr: "Fatal: wrong password or no key found\n"}
	repo := New(runner, testConfig())

	_, err := repo.Forget(context.Background(), ForgetOptions{Host: "h", KeepDaily: 1})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %v, want *ExitError", err)
	}
	if exitErr.Code != ExitWrongPassword {
		t.Errorf("Code = %s, want %s", exitErr.Code, ExitWrongPassword)
	}
}

func TestRepository_Forget_LockHeld_Classified(t *testing.T) {
	runner := &fakeRunner{exitCode: 11, stderr: "unable to create lock\nanother process is locking the repository\n"}
	repo := New(runner, testConfig())

	_, err := repo.Forget(context.Background(), ForgetOptions{Host: "h", KeepDaily: 1})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %v, want *ExitError", err)
	}
	if exitErr.Code != ExitLockFailed {
		t.Errorf("Code = %s, want %s", exitErr.Code, ExitLockFailed)
	}
}

func TestRepository_Forget_EmptyOutput_ReturnsZeroValueNotError(t *testing.T) {
	runner := &fakeRunner{stdout: ""}
	repo := New(runner, testConfig())

	summary, err := repo.Forget(context.Background(), ForgetOptions{Host: "h", KeepDaily: 1})
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if summary.KeptSnapshotIDs != nil || summary.RemovedSnapshotIDs != nil {
		t.Errorf("summary = %+v, want zero value for empty output", summary)
	}
}

func TestRepository_Forget_UnrecognizedJSON_ReturnsZeroValueNotError(t *testing.T) {
	// A future/unrecognized restic --json shape must not turn an
	// already-successful forget (exit 0) into a reported error -- see
	// ForgetSummary's doc comment.
	runner := &fakeRunner{stdout: `{"message_type":"unexpected_shape"}`}
	repo := New(runner, testConfig())

	summary, err := repo.Forget(context.Background(), ForgetOptions{Host: "h", KeepDaily: 1})
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if summary.KeptSnapshotIDs != nil || summary.RemovedSnapshotIDs != nil {
		t.Errorf("summary = %+v, want zero value for unrecognized output", summary)
	}
}

func TestRepository_Forget_MultipleGroupsSummed(t *testing.T) {
	runner := &fakeRunner{stdout: `[{"keep":[{"id":"k1"}],"remove":[{"id":"r1"}]},{"keep":[{"id":"k2"}],"remove":[{"id":"r2"},{"id":"r3"}]}]`}
	repo := New(runner, testConfig())

	summary, err := repo.Forget(context.Background(), ForgetOptions{Host: "h", KeepDaily: 1})
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if len(summary.KeptSnapshotIDs) != 2 {
		t.Errorf("KeptSnapshotIDs = %v, want 2 entries", summary.KeptSnapshotIDs)
	}
	if len(summary.RemovedSnapshotIDs) != 3 {
		t.Errorf("RemovedSnapshotIDs = %v, want 3 entries", summary.RemovedSnapshotIDs)
	}
}

func TestParseForgetJSON_FallsBackToShortID(t *testing.T) {
	summary, err := parseForgetJSON([]byte(`[{"keep":[],"remove":[{"short_id":"abcd1234"}]}]`))
	if err != nil {
		t.Fatalf("parseForgetJSON: %v", err)
	}
	if len(summary.RemovedSnapshotIDs) != 1 || summary.RemovedSnapshotIDs[0] != "abcd1234" {
		t.Errorf("RemovedSnapshotIDs = %v, want [abcd1234]", summary.RemovedSnapshotIDs)
	}
}
