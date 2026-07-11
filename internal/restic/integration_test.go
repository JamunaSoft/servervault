//go:build integration

package restic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/testsupport"
)

func TestIntegration_Backup_Check_Snapshots(t *testing.T) {
	cfg := testsupport.NewResticRepository(t)
	repo := New(execx.DefaultRunner{}, cfg)

	payloadDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(payloadDir, "file.txt"), []byte("servervault integration test payload"), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	summary, err := repo.Backup(context.Background(), BackupOptions{
		Paths:   []string{payloadDir},
		Tags:    []string{"servervault", "integration-test"},
		HostTag: "integration-test-host",
	})
	if err != nil {
		t.Fatalf("Backup(): unexpected error: %v", err)
	}
	if summary.SnapshotID == "" {
		t.Error("Summary.SnapshotID is empty")
	}
	if summary.FilesNew == 0 {
		t.Error("Summary.FilesNew = 0, want at least 1 (the payload file)")
	}
	if len(summary.Warnings) != 0 {
		t.Errorf("Summary.Warnings = %v, want none for a clean backup", summary.Warnings)
	}

	if err := repo.Check(context.Background(), CheckOptions{}); err != nil {
		t.Errorf("Check(): unexpected error: %v", err)
	}
	if err := repo.Check(context.Background(), CheckOptions{ReadData: true}); err != nil {
		t.Errorf("Check(ReadData: true): unexpected error: %v", err)
	}

	snapshots, err := repo.Snapshots(context.Background(), SnapshotsOptions{Host: "integration-test-host", Tags: []string{"servervault"}})
	if err != nil {
		t.Fatalf("Snapshots(): unexpected error: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("Snapshots() = %d entries, want 1", len(snapshots))
	}
	if snapshots[0].ID == "" || snapshots[0].Hostname != "integration-test-host" {
		t.Errorf("Snapshots()[0] = %+v, unexpected values", snapshots[0])
	}
}

func TestIntegration_CatConfig(t *testing.T) {
	cfg := testsupport.NewResticRepository(t)
	repo := New(execx.DefaultRunner{}, cfg)

	if err := repo.CatConfig(context.Background()); err != nil {
		t.Errorf("CatConfig(): unexpected error: %v", err)
	}
}

func TestIntegration_WrongPassword(t *testing.T) {
	cfg := testsupport.NewResticRepository(t)

	// A second, independently-generated password file pointed at the
	// same (already-initialized-with-a-different-password) repository.
	wrongPasswordFile := filepath.Join(t.TempDir(), "wrong-password")
	if err := os.WriteFile(wrongPasswordFile, []byte("definitely-not-the-real-password"), 0o600); err != nil {
		t.Fatalf("write wrong password file: %v", err)
	}
	wrongCfg := config.ResticConfig{
		Repository:   cfg.Repository,
		PasswordFile: wrongPasswordFile,
	}
	repo := New(execx.DefaultRunner{}, wrongCfg)

	err := repo.CatConfig(context.Background())
	if err == nil {
		t.Fatal("CatConfig() with the wrong password: want an error, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %v, want it to unwrap to *ExitError", err)
	}
	if exitErr.Code != ExitWrongPassword {
		t.Errorf("Code = %v, want ExitWrongPassword (got detail: %v)", exitErr.Code, err)
	}
}

func TestIntegration_Backup_NonexistentRepository(t *testing.T) {
	testsupport.RequireRestic(t)

	cfg := config.ResticConfig{
		Repository:   "local:" + filepath.Join(t.TempDir(), "never-initialized"),
		PasswordFile: filepath.Join(t.TempDir(), "password"),
	}
	if err := os.WriteFile(cfg.PasswordFile, []byte("irrelevant"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	repo := New(execx.DefaultRunner{}, cfg)

	_, err := repo.Backup(context.Background(), BackupOptions{Paths: []string{t.TempDir()}})
	if err == nil {
		t.Fatal("Backup() against a never-initialized repository: want an error, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %v, want it to unwrap to *ExitError", err)
	}
	if exitErr.Code != ExitRepositoryNotFound {
		t.Errorf("Code = %v, want ExitRepositoryNotFound (got detail: %v)", exitErr.Code, err)
	}
}
