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

func TestLoad_EnvBackupPathsWithSpaces(t *testing.T) {
	// A path containing a space must survive as a single entry — only a
	// comma separates SERVERVAULT_BACKUP_PATHS entries.
	t.Setenv("SERVERVAULT_BACKUP_PATHS", "/var/www/My Site,/etc/nginx")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): unexpected error: %v", err)
	}

	want := []string{"/var/www/My Site", "/etc/nginx"}
	if len(cfg.Backup.Paths) != len(want) || cfg.Backup.Paths[0] != want[0] || cfg.Backup.Paths[1] != want[1] {
		t.Errorf("Backup.Paths = %v, want %v", cfg.Backup.Paths, want)
	}
}

func TestSplitPaths(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "whitespace only", in: "   ", want: nil},
		{name: "single path", in: "/var/www", want: []string{"/var/www"}},
		{name: "comma separated", in: "/var/www,/etc/nginx", want: []string{"/var/www", "/etc/nginx"}},
		{name: "comma with surrounding spaces", in: "/var/www, /etc/nginx , /opt/app", want: []string{"/var/www", "/etc/nginx", "/opt/app"}},
		{name: "path containing a space is not split", in: "/var/www/My Site,/etc/nginx", want: []string{"/var/www/My Site", "/etc/nginx"}},
		{name: "trailing comma dropped", in: "/var/www,/etc/nginx,", want: []string{"/var/www", "/etc/nginx"}},
		{name: "leading comma dropped", in: ",/var/www", want: []string{"/var/www"}},
		{name: "multiple spaces preserved inside a path", in: "/mnt/Shared  Drive/data", want: []string{"/mnt/Shared  Drive/data"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitPaths(tt.in)
			if !equalStrings(got, tt.want) {
				t.Errorf("splitPaths(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSplitTags(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "comma separated", in: "servervault,production", want: []string{"servervault", "production"}},
		{name: "space separated", in: "servervault production", want: []string{"servervault", "production"}},
		{name: "mixed comma and space", in: "servervault, production hetzner", want: []string{"servervault", "production", "hetzner"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitTags(tt.in)
			if !equalStrings(got, tt.want) {
				t.Errorf("splitTags(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
