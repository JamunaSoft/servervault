package config

import "testing"

// validConfig returns a Config that should pass Validate cleanly; tests
// mutate a copy of it to exercise one failure at a time.
func validConfig() *Config {
	return &Config{
		Restic: ResticConfig{
			Repository:   "sftp:user@host:/backups/servervault",
			PasswordFile: "/etc/servervault/restic-password",
		},
		Postgres: PostgresConfig{
			Enabled:   true,
			Database:  "app_production",
			User:      "postgres",
			Port:      5432,
			ZstdLevel: 10,
		},
		Backup: BackupConfig{
			Paths:    []string{"/var/www"},
			Root:     "/var/backups/servervault",
			LockFile: "/run/lock/servervault-backup.lock",
		},
		Restore: RestoreConfig{
			StagingRoot:        "/var/restore/servervault",
			TempDatabasePrefix: "servervault_restore_",
			LockFile:           "/run/lock/servervault-restore.lock",
		},
		Retention: RetentionConfig{
			KeepDaily: 7,
		},
		StateDir: "/var/lib/servervault",
	}
}

func TestValidate_ValidConfigHasNoErrors(t *testing.T) {
	if errs := Validate(validConfig()); len(errs) != 0 {
		t.Fatalf("Validate(validConfig()) = %v, want no errors", errs)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Config)
		wantField string
	}{
		{
			name:      "empty repository",
			mutate:    func(c *Config) { c.Restic.Repository = "" },
			wantField: "restic.repository",
		},
		{
			name:      "unrecognized repository backend",
			mutate:    func(c *Config) { c.Restic.Repository = "ftp://example.com/backups" },
			wantField: "restic.repository",
		},
		{
			name:      "relative password file",
			mutate:    func(c *Config) { c.Restic.PasswordFile = "restic-password" },
			wantField: "restic.password_file",
		},
		{
			name:      "no backup paths",
			mutate:    func(c *Config) { c.Backup.Paths = nil },
			wantField: "backup.paths",
		},
		{
			name:      "relative backup path",
			mutate:    func(c *Config) { c.Backup.Paths = []string{"var/www"} },
			wantField: "backup.paths",
		},
		{
			name:      "empty lock file",
			mutate:    func(c *Config) { c.Backup.LockFile = "" },
			wantField: "backup.lock_file",
		},
		{
			name:      "relative lock file",
			mutate:    func(c *Config) { c.Backup.LockFile = "servervault-backup.lock" },
			wantField: "backup.lock_file",
		},
		{
			name:      "negative retention",
			mutate:    func(c *Config) { c.Retention.KeepDaily = -1 },
			wantField: "retention.keep_daily",
		},
		{
			name: "all retention zero",
			mutate: func(c *Config) {
				c.Retention.KeepDaily = 0
				c.Retention.KeepWeekly = 0
				c.Retention.KeepMonthly = 0
			},
			wantField: "retention",
		},
		{
			name:      "postgres enabled with no database",
			mutate:    func(c *Config) { c.Postgres.Database = "" },
			wantField: "postgres.database",
		},
		{
			name:      "postgres port out of range",
			mutate:    func(c *Config) { c.Postgres.Port = 70000 },
			wantField: "postgres.port",
		},
		{
			name:      "postgres zstd level out of range",
			mutate:    func(c *Config) { c.Postgres.ZstdLevel = 0 },
			wantField: "postgres.zstd_level",
		},
		{
			name:      "empty staging root",
			mutate:    func(c *Config) { c.Restore.StagingRoot = "" },
			wantField: "restore.staging_root",
		},
		{
			name:      "staging root is root path",
			mutate:    func(c *Config) { c.Restore.StagingRoot = "/" },
			wantField: "restore.staging_root",
		},
		{
			name: "staging root equals a live backup path",
			mutate: func(c *Config) {
				c.Backup.Paths = []string{"/var/www"}
				c.Restore.StagingRoot = "/var/www"
			},
			wantField: "restore.staging_root",
		},
		{
			name: "staging root nested inside a live backup path",
			mutate: func(c *Config) {
				c.Backup.Paths = []string{"/var/www"}
				c.Restore.StagingRoot = "/var/www/restore"
			},
			wantField: "restore.staging_root",
		},
		{
			name: "live backup path nested inside staging root",
			mutate: func(c *Config) {
				c.Backup.Paths = []string{"/var/restore/servervault/app"}
				c.Restore.StagingRoot = "/var/restore/servervault"
			},
			wantField: "restore.staging_root",
		},
		{
			name: "staging root with trailing slash still overlaps",
			mutate: func(c *Config) {
				c.Backup.Paths = []string{"/var/www"}
				c.Restore.StagingRoot = "/var/www/"
			},
			wantField: "restore.staging_root",
		},
		{
			name:      "temp database prefix equals live database",
			mutate:    func(c *Config) { c.Restore.TempDatabasePrefix = c.Postgres.Database },
			wantField: "restore.temp_database_prefix",
		},
		{
<<<<<<< HEAD
=======
			name:      "empty restore lock file",
			mutate:    func(c *Config) { c.Restore.LockFile = "" },
			wantField: "restore.lock_file",
		},
		{
			name:      "relative restore lock file",
			mutate:    func(c *Config) { c.Restore.LockFile = "servervault-restore.lock" },
			wantField: "restore.lock_file",
		},
		{
>>>>>>> 4c2dfaf (feat(config): add restore lock file and state directory)
			name:      "empty state dir",
			mutate:    func(c *Config) { c.StateDir = "" },
			wantField: "state_dir",
		},
		{
			name:      "relative state dir",
			mutate:    func(c *Config) { c.StateDir = "var/lib/servervault" },
			wantField: "state_dir",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)

			errs := Validate(cfg)
			if len(errs) == 0 {
				t.Fatalf("Validate(): want an error for field %q, got none", tt.wantField)
			}
			if !containsField(errs, tt.wantField) {
				t.Errorf("Validate() = %v, want an error for field %q", errs, tt.wantField)
			}
		})
	}
}

func TestValidate_DeceptivePrefixIsNotFlagged(t *testing.T) {
	// /var/www-old is not nested inside /var/www even though it shares a
	// string prefix — a raw strings.HasPrefix check would wrongly flag
	// this as an overlap.
	cfg := validConfig()
	cfg.Backup.Paths = []string{"/var/www"}
	cfg.Restore.StagingRoot = "/var/www-old"

	if errs := Validate(cfg); containsField(errs, "restore.staging_root") {
		t.Errorf("Validate() flagged a deceptive-prefix path as overlapping: %v", errs)
	}
}

func TestPathsOverlap(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "equal", a: "/var/www", b: "/var/www", want: true},
		{name: "a nested inside b", a: "/var/www/restore", b: "/var/www", want: true},
		{name: "b nested inside a", a: "/var/www", b: "/var/www/restore", want: true},
		{name: "nested one level deeper", a: "/var/restore", b: "/var/restore/app", want: true},
		{name: "sibling paths", a: "/var/www", b: "/var/nginx", want: false},
		{name: "sibling paths under restore", a: "/var/restore/app", b: "/var/restore/other", want: false},
		{name: "deceptive prefix, a shorter", a: "/var/www", b: "/var/www-old", want: false},
		{name: "deceptive prefix, b shorter", a: "/var/www-old", b: "/var/www", want: false},
		{name: "trailing slash on a, otherwise equal", a: "/var/www/", b: "/var/www", want: true},
		{name: "trailing slash on b, otherwise equal", a: "/var/www", b: "/var/www/", want: true},
		{name: "trailing slash with nesting", a: "/var/www/restore/", b: "/var/www/", want: true},
		{name: "unclean path with dot segments", a: "/var/www/../www2", b: "/var/www2", want: true},
		{name: "unclean path with current-dir segment", a: "/var/www/./sub", b: "/var/www/sub", want: true},
		{name: "root vs anything", a: "/", b: "/var/www", want: true},
		{name: "completely unrelated", a: "/etc/nginx", b: "/opt/app", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PathsOverlap(tt.a, tt.b); got != tt.want {
				t.Errorf("PathsOverlap(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
			// Overlap must be symmetric.
			if got := PathsOverlap(tt.b, tt.a); got != tt.want {
				t.Errorf("PathsOverlap(%q, %q) [swapped] = %v, want %v", tt.b, tt.a, got, tt.want)
			}
		})
	}
}

func TestValidate_PostgresDisabledSkipsPostgresChecks(t *testing.T) {
	cfg := validConfig()
	cfg.Postgres.Enabled = false
	cfg.Postgres.Database = ""
	cfg.Postgres.User = ""

	if errs := Validate(cfg); containsField(errs, "postgres.database") || containsField(errs, "postgres.user") {
		t.Errorf("Validate() with postgres.enabled=false: want no postgres.* errors, got %v", errs)
	}
}

func TestValidationErrors_Error(t *testing.T) {
	errs := ValidationErrors{
		{Field: "a", Message: "bad"},
		{Field: "b", Message: "worse"},
	}
	want := "a: bad; b: worse"
	if got := errs.Error(); got != want {
		t.Errorf("ValidationErrors.Error() = %q, want %q", got, want)
	}
}

func containsField(errs ValidationErrors, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}
