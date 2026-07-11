//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/execx"
	"github.com/JamunaSoft/servervault/internal/testsupport"
)

func TestIntegration_Ping(t *testing.T) {
	cfg := testsupport.NewPostgresDatabase(t)
	c := New(execx.DefaultRunner{}, cfg)

	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping(): unexpected error: %v", err)
	}
}

func TestIntegration_Ping_NonexistentDatabase(t *testing.T) {
	testsupport.RequirePostgresBinaries(t)

	cfg := config.PostgresConfig{
		Enabled:  true,
		Database: "servervault_test_definitely_does_not_exist",
		User:     testsupport.TestPostgresUser(),
	}
	c := New(execx.DefaultRunner{}, cfg)

	if err := c.Ping(context.Background()); err == nil {
		t.Error("Ping() against a nonexistent database: want an error, got nil")
	}
}

func TestIntegration_DumpAndVerify(t *testing.T) {
	cfg := testsupport.NewPostgresDatabase(t)
	c := New(execx.DefaultRunner{}, cfg)

	dir := t.TempDir()
	meta, err := c.Dump(context.Background(), dir)
	if err != nil {
		t.Fatalf("Dump(): unexpected error: %v", err)
	}
	t.Cleanup(func() { os.Remove(meta.Path) })

	if meta.Bytes == 0 {
		t.Error("Metadata.Bytes = 0, want > 0")
	}
	info, err := os.Stat(meta.Path)
	if err != nil {
		t.Fatalf("stat dump file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("dump file mode = %s, want 0600", perm)
	}

	if err := c.VerifyDump(context.Background(), meta.Path); err != nil {
		t.Errorf("VerifyDump(): unexpected error: %v", err)
	}

	remaining, _ := filepath.Glob(filepath.Join(dir, "verify-*"))
	if len(remaining) != 0 {
		t.Errorf("temporary decompressed file(s) left behind: %v", remaining)
	}
}

func TestIntegration_Dump_NonexistentDatabase(t *testing.T) {
	testsupport.RequirePostgresBinaries(t)

	cfg := config.PostgresConfig{
		Enabled:   true,
		Database:  "servervault_test_definitely_does_not_exist",
		User:      testsupport.TestPostgresUser(),
		ZstdLevel: 10,
	}
	c := New(execx.DefaultRunner{}, cfg)

	dir := t.TempDir()
	meta, err := c.Dump(context.Background(), dir)
	if err == nil {
		t.Fatal("Dump() against a nonexistent database: want an error, got nil")
	}
	var dumpErr *DumpError
	if !errors.As(err, &dumpErr) {
		t.Fatalf("error = %v, want it to unwrap to *DumpError", err)
	}
	if dumpErr.Stage != "pg_dump" {
		t.Errorf("Stage = %q, want %q", dumpErr.Stage, "pg_dump")
	}
	// The (partial/empty) destination path is still reported so the
	// caller can clean it up -- mirrors internal/backup's own cleanup
	// contract.
	if meta.Path != "" {
		_ = os.Remove(meta.Path)
	}
}

func TestIntegration_VerifyDump_CorruptedFile(t *testing.T) {
	cfg := testsupport.NewPostgresDatabase(t)
	c := New(execx.DefaultRunner{}, cfg)

	dir := t.TempDir()
	meta, err := c.Dump(context.Background(), dir)
	if err != nil {
		t.Fatalf("Dump(): unexpected error: %v", err)
	}
	t.Cleanup(func() { os.Remove(meta.Path) })

	// Corrupt the compressed dump in place.
	data, err := os.ReadFile(meta.Path)
	if err != nil {
		t.Fatalf("read dump file: %v", err)
	}
	if len(data) < 16 {
		t.Fatalf("dump file too small to corrupt meaningfully: %d bytes", len(data))
	}
	corrupted := append([]byte{}, data...)
	for i := 4; i < 12; i++ {
		corrupted[i] ^= 0xFF
	}
	if err := os.WriteFile(meta.Path, corrupted, 0o600); err != nil {
		t.Fatalf("write corrupted dump file: %v", err)
	}

	err = c.VerifyDump(context.Background(), meta.Path)
	if err == nil {
		t.Fatal("VerifyDump() on a corrupted file: want an error, got nil")
	}
	// Could fail at decompression or at pg_restore --list depending on
	// exactly which bytes were flipped -- either is a correct outcome
	// here; what matters is cleanup.
	var verifyErr *VerifyError
	if !errors.As(err, &verifyErr) {
		t.Fatalf("error = %v, want it to unwrap to *VerifyError", err)
	}
	if verifyErr.Stage != "decompress" && verifyErr.Stage != "pg_restore" {
		t.Errorf("Stage = %q, want \"decompress\" or \"pg_restore\"", verifyErr.Stage)
	}

	remaining, _ := filepath.Glob(filepath.Join(dir, "verify-*"))
	if len(remaining) != 0 {
		t.Errorf("temporary decompressed file(s) left behind after a verify failure: %v", remaining)
	}
}

func TestIntegration_DatabaseNamePrefixEnforced(t *testing.T) {
	// A cheap sanity check that testsupport really does generate
	// servervault_test_-prefixed names, since NewPostgresDatabase's
	// cleanup safety guard depends on it.
	cfg := testsupport.NewPostgresDatabase(t)
	if !strings.HasPrefix(cfg.Database, "servervault_test_") {
		t.Errorf("test database name = %q, want the servervault_test_ prefix", cfg.Database)
	}
}
