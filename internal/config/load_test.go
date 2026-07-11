package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_NoFile(t *testing.T) {
	// A missing file at a non-default, non-explicit path... Load only
	// treats DefaultPath specially, so point explicitPath at "" and rely
	// on DefaultPath being absent in the test sandbox (it is: no test
	// writes to /etc/servervault).
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") with no file present: unexpected error: %v", err)
	}
	if cfg.Retention.KeepDaily != Defaults().Retention.KeepDaily {
		t.Errorf("Load(\"\") without a YAML file should equal Defaults(); KeepDaily = %d, want %d",
			cfg.Retention.KeepDaily, Defaults().Retention.KeepDaily)
	}
}

func TestLoad_ExplicitMissingFileIsError(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("Load() with an explicit missing path: want error, got nil")
	}
}

func TestLoad_YAMLOverridesDefaults(t *testing.T) {
	path := writeYAML(t, `
restic:
  repository: "sftp:user@host:/backups/servervault"
  password_file: "/etc/servervault/restic-password"
backup:
  paths:
    - /var/www
  root: /var/backups/servervault
retention:
  keep_daily: 3
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error: %v", path, err)
	}

	if cfg.Restic.Repository != "sftp:user@host:/backups/servervault" {
		t.Errorf("Restic.Repository = %q, want the YAML value", cfg.Restic.Repository)
	}
	if cfg.Retention.KeepDaily != 3 {
		t.Errorf("Retention.KeepDaily = %d, want 3 (from YAML)", cfg.Retention.KeepDaily)
	}
	// Untouched by YAML: should still carry the default.
	if cfg.Retention.KeepWeekly != Defaults().Retention.KeepWeekly {
		t.Errorf("Retention.KeepWeekly = %d, want default %d", cfg.Retention.KeepWeekly, Defaults().Retention.KeepWeekly)
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	path := writeYAML(t, "restic: [this is not a mapping")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() with malformed YAML: want error, got nil")
	}
}

func TestLoad_EnvOverridesYAML(t *testing.T) {
	path := writeYAML(t, `
retention:
  keep_daily: 3
`)
	t.Setenv("SERVERVAULT_RETENTION_KEEP_DAILY", "9")
	t.Setenv("SERVERVAULT_BACKUP_PATHS", "/var/www, /etc/nginx")
	t.Setenv("SERVERVAULT_POSTGRES_ENABLED", "false")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q): unexpected error: %v", path, err)
	}

	if cfg.Retention.KeepDaily != 9 {
		t.Errorf("Retention.KeepDaily = %d, want 9 (env overrides YAML)", cfg.Retention.KeepDaily)
	}
	wantPaths := []string{"/var/www", "/etc/nginx"}
	if len(cfg.Backup.Paths) != len(wantPaths) || cfg.Backup.Paths[0] != wantPaths[0] || cfg.Backup.Paths[1] != wantPaths[1] {
		t.Errorf("Backup.Paths = %v, want %v", cfg.Backup.Paths, wantPaths)
	}
	if cfg.Postgres.Enabled {
		t.Error("Postgres.Enabled = true, want false (from SERVERVAULT_POSTGRES_ENABLED)")
	}
}

func TestLoad_EnvInvalidInt(t *testing.T) {
	t.Setenv("SERVERVAULT_RETENTION_KEEP_DAILY", "not-a-number")

	_, err := Load("")
	if err == nil {
		t.Fatal("Load() with an invalid integer env var: want error, got nil")
	}
}

func TestApplyEnv_AllKnownVarsUnset(t *testing.T) {
	// Sanity check: applyEnv must be a pure no-op (beyond what Defaults()
	// already set) when none of the SERVERVAULT_* variables are present,
	// so Load("") == Defaults() by construction, not by accident.
	cfg := Defaults()
	if err := applyEnv(cfg); err != nil {
		t.Fatalf("applyEnv() with no env vars set: unexpected error: %v", err)
	}
	defaults := Defaults()
	if cfg.Restic.Repository != defaults.Restic.Repository ||
		cfg.Postgres.Port != defaults.Postgres.Port ||
		cfg.Retention.KeepDaily != defaults.Retention.KeepDaily {
		t.Error("applyEnv() modified cfg despite no SERVERVAULT_* variables being set")
	}
}

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "servervault.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeYAML: %v", err)
	}
	return path
}
