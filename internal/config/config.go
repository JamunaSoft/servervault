// Package config loads and validates ServerVault's layered configuration:
// safe defaults, overridden by a YAML file, overridden by environment
// variables. CLI flags are the caller's responsibility to apply on top of
// the *Config this package returns, keeping this package independent of
// Cobra.
//
// See docs/configuration.md and configs/servervault.example.yaml for the
// user-facing shape this mirrors field-for-field.
package config

// Config is ServerVault's fully-resolved configuration.
type Config struct {
	Restic    ResticConfig    `yaml:"restic"`
	Postgres  PostgresConfig  `yaml:"postgres"`
	Backup    BackupConfig    `yaml:"backup"`
	Restore   RestoreConfig   `yaml:"restore"`
	Retention RetentionConfig `yaml:"retention"`
	HostTag   string          `yaml:"host_tag"`
	Notify    NotifyConfig    `yaml:"notify"`
}

// ResticConfig configures the Restic repository backup/restore operate against.
type ResticConfig struct {
	Repository   string   `yaml:"repository"`
	PasswordFile string   `yaml:"password_file"`
	SFTPCommand  string   `yaml:"sftp_command"`
	Tags         []string `yaml:"tags"`
}

// PostgresConfig configures the PostgreSQL database dumped and verified as
// part of a backup.
//
// Host and Port default to empty/unset, not "127.0.0.1"/5432: an empty
// Host means "connect via the local Unix socket," which is what enables
// peer authentication (sudo -u <user> psql/pg_dump with no password at
// all) — the same auth model the shell implementation relies on. Setting
// Host explicitly switches to a TCP connection, which requires a
// different, password-based auth method that ServerVault does not
// implement; see internal/postgres.
type PostgresConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Database  string `yaml:"database"`
	User      string `yaml:"user"`
	Host      string `yaml:"host"`
	Port      int    `yaml:"port"`
	ZstdLevel int    `yaml:"zstd_level"`
}

// BackupConfig configures which paths are backed up alongside the database
// dump.
type BackupConfig struct {
	Paths       []string `yaml:"paths"`
	ExcludeFile string   `yaml:"exclude_file"`
	Root        string   `yaml:"root"`
	// LockFile prevents concurrent backups (internal/lock). It
	// deliberately defaults to the same path the shell implementation's
	// servervault-backup uses, so a shell-driven and a Go-driven backup
	// mutually exclude each other during a shell-to-Go migration period.
	LockFile string `yaml:"lock_file"`
}

// RestoreConfig configures where restores land by default. Restores never
// target a live path or a live database directly — see
// docs/security-model.md.
type RestoreConfig struct {
	StagingRoot        string `yaml:"staging_root"`
	TempDatabasePrefix string `yaml:"temp_database_prefix"`
}

// RetentionConfig configures how many snapshots `servervault prune` keeps.
type RetentionConfig struct {
	KeepDaily   int `yaml:"keep_daily"`
	KeepWeekly  int `yaml:"keep_weekly"`
	KeepMonthly int `yaml:"keep_monthly"`
}

// NotifyConfig configures optional failure notifications.
type NotifyConfig struct {
	Enabled    bool   `yaml:"enabled"`
	WebhookURL string `yaml:"webhook_url"`
}

// DefaultPath is where ServerVault looks for its YAML config when no
// explicit path is given, matching the shell implementation's
// /etc/servervault/ convention.
const DefaultPath = "/etc/servervault/servervault.yaml"

// Defaults returns a Config populated with ServerVault's safe defaults —
// the first, lowest-precedence layer described in docs/configuration.md.
func Defaults() *Config {
	return &Config{
		Restic: ResticConfig{
			PasswordFile: "/etc/servervault/restic-password",
			Tags:         []string{"servervault"},
		},
		Postgres: PostgresConfig{
			Enabled:   true,
			User:      "postgres",
			Port:      5432,
			ZstdLevel: 10,
		},
		Backup: BackupConfig{
			ExcludeFile: "/etc/servervault/excludes.txt",
			Root:        "/var/backups/servervault",
			LockFile:    "/run/lock/servervault-backup.lock",
		},
		Restore: RestoreConfig{
			StagingRoot:        "/var/restore/servervault",
			TempDatabasePrefix: "servervault_restore_",
		},
		Retention: RetentionConfig{
			KeepDaily:   7,
			KeepWeekly:  4,
			KeepMonthly: 12,
		},
	}
}
