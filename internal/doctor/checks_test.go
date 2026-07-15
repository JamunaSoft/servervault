package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
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

// fakeResticAccessChecker and fakePostgresPinger let doctor tests exercise
// checkResticAccess/checkPostgresConnectivity (and Run() as a whole)
// without ever invoking a real restic/psql binary or touching the
// network.
type fakeResticAccessChecker struct{ err error }

func (f fakeResticAccessChecker) CatConfig(ctx context.Context) error { return f.err }

type fakePostgresPinger struct{ err error }

func (f fakePostgresPinger) Ping(ctx context.Context) error { return f.err }

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

func TestCheckRestoreStagingOverlap(t *testing.T) {
	t.Run("not configured", func(t *testing.T) {
		cfg := baseConfig()
		cfg.Restore.StagingRoot = ""

		check := checkRestoreStagingOverlap(Options{Config: cfg})
		if check.Status != StatusSkip {
			t.Errorf("status = %v, want StatusSkip (detail: %s)", check.Status, check.Detail)
		}
	})

	t.Run("staging root does not exist yet", func(t *testing.T) {
		dir := t.TempDir()
		cfg := baseConfig()
		cfg.Restore.StagingRoot = filepath.Join(dir, "not-created-yet")
		cfg.Backup.Paths = []string{dir}

		check := checkRestoreStagingOverlap(Options{Config: cfg})
		if check.Status != StatusSkip {
			t.Errorf("status = %v, want StatusSkip (detail: %s)", check.Status, check.Detail)
		}
	})

	t.Run("no overlap between separate real directories", func(t *testing.T) {
		root := t.TempDir()
		backupPath := filepath.Join(root, "www")
		staging := filepath.Join(root, "restore")
		mustMkdir(t, backupPath)
		mustMkdir(t, staging)

		cfg := baseConfig()
		cfg.Backup.Paths = []string{backupPath}
		cfg.Restore.StagingRoot = staging

		check := checkRestoreStagingOverlap(Options{Config: cfg})
		if check.Status != StatusOK {
			t.Errorf("status = %v, want StatusOK (detail: %s)", check.Status, check.Detail)
		}
	})

	t.Run("symlink makes staging root physically overlap a backup path", func(t *testing.T) {
		root := t.TempDir()
		realBackupDir := filepath.Join(root, "actual-www")
		mustMkdir(t, realBackupDir)

		// staging root LOOKS separate as a string, but is a symlink
		// pointing straight into the real backup directory.
		stagingSymlink := filepath.Join(root, "staging-symlink")
		if err := os.Symlink(realBackupDir, stagingSymlink); err != nil {
			t.Skipf("symlinks not supported in this environment: %v", err)
		}

		cfg := baseConfig()
		cfg.Backup.Paths = []string{realBackupDir}
		cfg.Restore.StagingRoot = stagingSymlink

		check := checkRestoreStagingOverlap(Options{Config: cfg})
		if check.Status != StatusFail {
			t.Errorf("status = %v, want StatusFail for a symlink-induced overlap (detail: %s)", check.Status, check.Detail)
		}
	})

	t.Run("unresolvable backup path does not block a clean staging root", func(t *testing.T) {
		root := t.TempDir()
		staging := filepath.Join(root, "restore")
		mustMkdir(t, staging)

		cfg := baseConfig()
		cfg.Backup.Paths = []string{filepath.Join(root, "does-not-exist")}
		cfg.Restore.StagingRoot = staging

		check := checkRestoreStagingOverlap(Options{Config: cfg})
		if check.Status != StatusOK {
			t.Errorf("status = %v, want StatusOK when the unresolved backup path just doesn't exist yet (detail: %s)", check.Status, check.Detail)
		}
	})
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mustMkdir(%q): %v", path, err)
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

func TestCheckResticAccess(t *testing.T) {
	tests := []struct {
		name       string
		mutateCfg  func(*config.Config)
		restic     ResticAccessChecker
		wantStatus Status
	}{
		{
			name:       "not configured",
			mutateCfg:  func(c *config.Config) { c.Restic.Repository = "" },
			wantStatus: StatusSkip,
		},
		{
			name:       "no client available",
			restic:     nil,
			wantStatus: StatusSkip,
		},
		{
			name:       "reachable",
			restic:     fakeResticAccessChecker{},
			wantStatus: StatusOK,
		},
		{
			name:       "unreachable",
			restic:     fakeResticAccessChecker{err: errors.New("wrong password")},
			wantStatus: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			if tt.mutateCfg != nil {
				tt.mutateCfg(cfg)
			}
			check := checkResticAccess(context.Background(), Options{Config: cfg, Restic: tt.restic})
			if check.Status != tt.wantStatus {
				t.Errorf("checkResticAccess() status = %v, want %v (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
		})
	}
}

func TestCheckPostgresConnectivity(t *testing.T) {
	tests := []struct {
		name       string
		mutateCfg  func(*config.Config)
		postgres   PostgresPinger
		wantStatus Status
	}{
		{
			name:       "disabled",
			mutateCfg:  func(c *config.Config) { c.Postgres.Enabled = false },
			wantStatus: StatusSkip,
		},
		{
			name:       "no client available",
			postgres:   nil,
			wantStatus: StatusSkip,
		},
		{
			name:       "reachable",
			postgres:   fakePostgresPinger{},
			wantStatus: StatusOK,
		},
		{
			name:       "unreachable",
			postgres:   fakePostgresPinger{err: errors.New("connection refused")},
			wantStatus: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			if tt.mutateCfg != nil {
				tt.mutateCfg(cfg)
			}
			check := checkPostgresConnectivity(context.Background(), Options{Config: cfg, Postgres: tt.postgres})
			if check.Status != tt.wantStatus {
				t.Errorf("checkPostgresConnectivity() status = %v, want %v (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
		})
	}
}

func TestCheckLockState(t *testing.T) {
	t.Run("not configured", func(t *testing.T) {
		cfg := baseConfig()
		cfg.Backup.LockFile = ""
		check := checkLockState(Options{Config: cfg})
		if check.Status != StatusSkip {
			t.Errorf("status = %v, want StatusSkip", check.Status)
		}
	})

	t.Run("free", func(t *testing.T) {
		cfg := baseConfig()
		cfg.Backup.LockFile = filepath.Join(t.TempDir(), "backup.lock")
		check := checkLockState(Options{Config: cfg})
		if check.Status != StatusOK {
			t.Errorf("status = %v, want StatusOK (detail: %s)", check.Status, check.Detail)
		}
	})

	t.Run("held", func(t *testing.T) {
		cfg := baseConfig()
		cfg.Backup.LockFile = filepath.Join(t.TempDir(), "backup.lock")

		held, err := lock.Acquire(cfg.Backup.LockFile)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		defer held.Release()

		check := checkLockState(Options{Config: cfg})
		if check.Status != StatusWarn {
			t.Errorf("status = %v, want StatusWarn while held (detail: %s)", check.Status, check.Detail)
		}
	})
}

// fakeJobLister is a health.JobLister test double.
type fakeJobLister struct {
	jobs map[job.Type]job.Job
}

func (f fakeJobLister) LatestByType(_ context.Context, t job.Type) (job.Job, error) {
	j, ok := f.jobs[t]
	if !ok {
		return job.Job{}, job.ErrNotFound
	}
	return j, nil
}

func TestCheckOperationalHealth(t *testing.T) {
	t.Run("all healthy", func(t *testing.T) {
		cfg := baseConfig()
		cfg.Backup.LockFile = filepath.Join(t.TempDir(), "backup.lock")
		cfg.Restore.LockFile = filepath.Join(t.TempDir(), "restore.lock")
		cfg.Retention.LockFile = filepath.Join(t.TempDir(), "prune.lock")

		check := checkOperationalHealth(context.Background(), Options{
			Config: cfg,
			Restic: fakeResticAccessChecker{},
			Jobs: fakeJobLister{jobs: map[job.Type]job.Job{
				job.TypeBackup: {Type: job.TypeBackup, State: job.StateCompleted, FinishedAt: time.Now()},
			}},
		})
		if check.Status != StatusOK {
			t.Errorf("status = %v, want StatusOK (detail: %s)", check.Status, check.Detail)
		}
	})

	t.Run("no job history yet is not a failure", func(t *testing.T) {
		cfg := baseConfig()
		cfg.Backup.LockFile = filepath.Join(t.TempDir(), "backup.lock")
		cfg.Restore.LockFile = filepath.Join(t.TempDir(), "restore.lock")
		cfg.Retention.LockFile = filepath.Join(t.TempDir(), "prune.lock")

		check := checkOperationalHealth(context.Background(), Options{
			Config: cfg,
			Restic: fakeResticAccessChecker{},
			Jobs:   fakeJobLister{jobs: map[job.Type]job.Job{}},
		})
		if check.Status != StatusOK {
			t.Errorf("status = %v, want StatusOK -- no job history is StatusUnknown in internal/health, which must not fail doctor (detail: %s)", check.Status, check.Detail)
		}
	})

	t.Run("a failed last backup surfaces as FAIL", func(t *testing.T) {
		cfg := baseConfig()
		cfg.Backup.LockFile = filepath.Join(t.TempDir(), "backup.lock")
		cfg.Restore.LockFile = filepath.Join(t.TempDir(), "restore.lock")
		cfg.Retention.LockFile = filepath.Join(t.TempDir(), "prune.lock")

		check := checkOperationalHealth(context.Background(), Options{
			Config: cfg,
			Restic: fakeResticAccessChecker{},
			Jobs: fakeJobLister{jobs: map[job.Type]job.Job{
				job.TypeBackup: {Type: job.TypeBackup, State: job.StateFailed, FinishedAt: time.Now(), ErrorSummary: "disk full"},
			}},
		})
		if check.Status != StatusFail {
			t.Errorf("status = %v, want StatusFail", check.Status)
		}
		if !strings.Contains(check.Detail, "disk full") {
			t.Errorf("Detail = %q, want it to mention the failure reason", check.Detail)
		}
	})

	t.Run("a held restore lock surfaces as WARN, not a separate FAIL", func(t *testing.T) {
		cfg := baseConfig()
		cfg.Backup.LockFile = filepath.Join(t.TempDir(), "backup.lock")
		cfg.Restore.LockFile = filepath.Join(t.TempDir(), "restore.lock")
		cfg.Retention.LockFile = filepath.Join(t.TempDir(), "prune.lock")

		held, err := lock.Acquire(cfg.Restore.LockFile)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		defer held.Release()

		check := checkOperationalHealth(context.Background(), Options{
			Config: cfg,
			Restic: fakeResticAccessChecker{},
			Jobs:   fakeJobLister{jobs: map[job.Type]job.Job{}},
		})
		if check.Status != StatusWarn {
			t.Errorf("status = %v, want StatusWarn", check.Status)
		}
	})
}

// TestRun_OpensJobStoreFromStateDirWhenNil proves Run wires a real
// job.Store into checkOperationalHealth when Options.Jobs is left nil,
// the same nil-fallback convention Restic/Postgres already have --
// exercised via the operational-health check's own detail text rather
// than a job.Store-specific assertion, since Run doesn't expose the
// store it opened.
func TestRun_OpensJobStoreFromStateDirWhenNil(t *testing.T) {
	cfg := baseConfig()
	cfg.StateDir = t.TempDir()

	report := Run(context.Background(), Options{
		Config:   cfg,
		Restic:   fakeResticAccessChecker{},
		Postgres: fakePostgresPinger{},
	})

	for _, c := range report.Checks {
		if c.Name == "operational health (internal/health)" {
			if c.Status == StatusFail {
				t.Errorf("operational health check failed unexpectedly: %s", c.Detail)
			}
			return
		}
	}
	t.Error("Run() report missing the operational health check")
}
