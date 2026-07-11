package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/JamunaSoft/servervault/internal/config"
)

// fakeCommandChecker is an execx.CommandChecker test double so
// checkRequiredCommands doesn't depend on which binaries happen to be
// installed on the machine running the tests.
type fakeCommandChecker struct {
	found map[string]string
}

func (f fakeCommandChecker) LookPath(name string) (string, error) {
	if p, ok := f.found[name]; ok {
		return p, nil
	}
	return "", fmt.Errorf("%s: executable file not found in $PATH", name)
}

func baseConfig() *config.Config {
	cfg := config.Defaults()
	cfg.Restic.Repository = "sftp:user@host:/backups/servervault"
	cfg.Backup.Paths = []string{"/tmp"}
	cfg.Postgres.Database = "app_production"
	return cfg
}

func TestCheckRequiredCommands(t *testing.T) {
	tests := []struct {
		name       string
		found      map[string]string
		mutateCfg  func(*config.Config)
		wantStatus Status
	}{
		{
			name: "all present, postgres and sftp required",
			found: map[string]string{
				"restic": "/usr/bin/restic", "zstd": "/usr/bin/zstd",
				"pg_dump": "/usr/bin/pg_dump", "pg_restore": "/usr/bin/pg_restore",
				"ssh": "/usr/bin/ssh",
			},
			wantStatus: StatusOK,
		},
		{
			name:       "restic missing",
			found:      map[string]string{"zstd": "/usr/bin/zstd", "pg_dump": "/x", "pg_restore": "/x", "ssh": "/x"},
			wantStatus: StatusFail,
		},
		{
			name: "postgres disabled: pg_dump not required",
			found: map[string]string{
				"restic": "/usr/bin/restic", "zstd": "/usr/bin/zstd", "ssh": "/usr/bin/ssh",
			},
			mutateCfg:  func(c *config.Config) { c.Postgres.Enabled = false },
			wantStatus: StatusOK,
		},
		{
			name:       "local repository: ssh not required",
			found:      map[string]string{"restic": "/usr/bin/restic", "zstd": "/usr/bin/zstd", "pg_dump": "/x", "pg_restore": "/x"},
			mutateCfg:  func(c *config.Config) { c.Restic.Repository = "/mnt/backups" },
			wantStatus: StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			if tt.mutateCfg != nil {
				tt.mutateCfg(cfg)
			}
			opts := Options{Config: cfg, Commands: fakeCommandChecker{found: tt.found}}

			check := checkRequiredCommands(opts)
			if check.Status != tt.wantStatus {
				t.Errorf("checkRequiredCommands() status = %v, want %v (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
		})
	}
}

func TestCheckConfigValidation(t *testing.T) {
	valid := baseConfig()
	if check := checkConfigValidation(Options{Config: valid}); check.Status != StatusOK {
		t.Errorf("checkConfigValidation(valid) status = %v, want StatusOK (detail: %s)", check.Status, check.Detail)
	}

	invalid := baseConfig()
	invalid.Retention.KeepDaily = -1
	if check := checkConfigValidation(Options{Config: invalid}); check.Status != StatusFail {
		t.Errorf("checkConfigValidation(invalid) status = %v, want StatusFail", check.Status)
	}
}

func TestCheckSecretPermissions(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name       string
		setup      func() string // returns the password file path
		wantStatus Status
	}{
		{
			name: "not configured",
			setup: func() string {
				return ""
			},
			wantStatus: StatusFail,
		},
		{
			name: "does not exist",
			setup: func() string {
				return filepath.Join(dir, "missing-password")
			},
			wantStatus: StatusFail,
		},
		{
			name: "mode 0600",
			setup: func() string {
				p := filepath.Join(dir, "ok-password")
				if err := os.WriteFile(p, []byte("secret"), 0o600); err != nil {
					t.Fatalf("setup: %v", err)
				}
				return p
			},
			wantStatus: StatusOK,
		},
		{
			name: "world readable",
			setup: func() string {
				p := filepath.Join(dir, "bad-password")
				if err := os.WriteFile(p, []byte("secret"), 0o644); err != nil {
					t.Fatalf("setup: %v", err)
				}
				return p
			},
			wantStatus: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Restic.PasswordFile = tt.setup()

			check := checkSecretPermissions(Options{Config: cfg})
			if check.Status != tt.wantStatus {
				t.Errorf("checkSecretPermissions() status = %v, want %v (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
		})
	}
}

func TestCheckBackupPaths(t *testing.T) {
	existing := t.TempDir()

	tests := []struct {
		name       string
		paths      []string
		wantStatus Status
	}{
		{name: "none configured", paths: nil, wantStatus: StatusWarn},
		{name: "all present", paths: []string{existing}, wantStatus: StatusOK},
		{name: "missing path", paths: []string{existing, "/does/not/exist/servervault-test"}, wantStatus: StatusFail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Backup.Paths = tt.paths

			check := checkBackupPaths(Options{Config: cfg})
			if check.Status != tt.wantStatus {
				t.Errorf("checkBackupPaths() status = %v, want %v (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
		})
	}
}

func TestCheckDiskSpace(t *testing.T) {
	existing := t.TempDir()

	tests := []struct {
		name       string
		root       string
		freeBytes  func(string) (uint64, error)
		wantStatus Status
	}{
		{
			name:       "not configured",
			root:       "",
			wantStatus: StatusSkip,
		},
		{
			name:       "does not exist yet",
			root:       filepath.Join(existing, "not-created-yet"),
			wantStatus: StatusWarn,
		},
		{
			name:       "plenty of free space",
			root:       existing,
			freeBytes:  func(string) (uint64, error) { return 10 << 30, nil },
			wantStatus: StatusOK,
		},
		{
			name:       "low free space",
			root:       existing,
			freeBytes:  func(string) (uint64, error) { return 100 << 20, nil }, // 100 MiB
			wantStatus: StatusFail,
		},
		{
			name:       "statfs unsupported",
			root:       existing,
			freeBytes:  func(string) (uint64, error) { return 0, errors.New("not supported") },
			wantStatus: StatusSkip,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Backup.Root = tt.root
			opts := Options{Config: cfg, FreeBytes: tt.freeBytes}
			if opts.FreeBytes == nil {
				opts.FreeBytes = func(string) (uint64, error) { return 0, nil }
			}

			check := checkDiskSpace(opts)
			if check.Status != tt.wantStatus {
				t.Errorf("checkDiskSpace() status = %v, want %v (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
		})
	}
}

func TestCheckTimezone(t *testing.T) {
	check := checkTimezone()
	if check.Status != StatusOK {
		t.Errorf("checkTimezone() status = %v, want StatusOK", check.Status)
	}
	if check.Detail == "" {
		t.Error("checkTimezone() detail is empty")
	}
}

func TestCheckPlatform(t *testing.T) {
	check := checkPlatform()
	if check.Detail == "" {
		t.Error("checkPlatform() detail is empty")
	}
	if check.Status != StatusOK && check.Status != StatusWarn {
		t.Errorf("checkPlatform() status = %v, want StatusOK or StatusWarn", check.Status)
	}
}
