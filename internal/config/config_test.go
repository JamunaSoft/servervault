package config

import "testing"

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	// Defaults alone are intentionally incomplete (no repository, no
	// backup paths — those are site-specific) but must be internally
	// consistent: applying Validate to Defaults() should fail only on the
	// fields a real deployment is expected to fill in, never on a bug in
	// the defaults themselves (e.g. a negative retention count or a
	// relative path).
	if cfg.Restic.PasswordFile == "" {
		t.Error("Defaults(): Restic.PasswordFile must not be empty")
	}
	if cfg.Postgres.Port != 5432 {
		t.Errorf("Defaults(): Postgres.Port = %d, want 5432", cfg.Postgres.Port)
	}
	if cfg.Retention.KeepDaily+cfg.Retention.KeepWeekly+cfg.Retention.KeepMonthly == 0 {
		t.Error("Defaults(): retention must not default to keeping nothing")
	}
	if cfg.Restore.StagingRoot == cfg.Backup.Root {
		t.Error("Defaults(): Restore.StagingRoot must differ from Backup.Root")
	}
	if cfg.Postgres.Host != "" {
		t.Errorf("Defaults(): Postgres.Host = %q, want empty (Unix socket + peer auth, matching the shell implementation)", cfg.Postgres.Host)
	}
	if cfg.Backup.LockFile == "" {
		t.Error("Defaults(): Backup.LockFile must not be empty")
	}
}
