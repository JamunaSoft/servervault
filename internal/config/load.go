package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load resolves a Config by layering, in increasing precedence:
//
//  1. Defaults
//  2. A YAML file
//  3. Environment variables (SERVERVAULT_*)
//
// explicitPath, when non-empty, is used as the YAML file path and it is an
// error if that file cannot be read or parsed. When explicitPath is empty,
// DefaultPath is tried instead, but a missing file at DefaultPath is not an
// error — Load falls back to defaults+environment only, so ServerVault
// keeps working with no YAML file present at all.
//
// CLI flags are not handled here; callers apply them on top of the
// returned Config, keeping this package independent of any flag library.
func Load(explicitPath string) (*Config, error) {
	cfg := Defaults()

	path := explicitPath
	required := explicitPath != ""
	if path == "" {
		path = DefaultPath
	}

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("config: parse %s: %w", path, err)
		}
	case os.IsNotExist(err) && !required:
		// No YAML file and none was explicitly requested: defaults +
		// environment only.
	default:
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	if err := applyEnv(cfg); err != nil {
		return nil, fmt.Errorf("config: environment: %w", err)
	}

	return cfg, nil
}

// applyEnv overrides cfg fields from SERVERVAULT_* environment variables,
// the third and highest-precedence layer Load applies. Unset variables
// leave the existing value untouched.
func applyEnv(cfg *Config) error {
	if v, ok := os.LookupEnv("SERVERVAULT_RESTIC_REPOSITORY"); ok {
		cfg.Restic.Repository = v
	}
	if v, ok := os.LookupEnv("SERVERVAULT_RESTIC_PASSWORD_FILE"); ok {
		cfg.Restic.PasswordFile = v
	}
	if v, ok := os.LookupEnv("SERVERVAULT_RESTIC_SFTP_COMMAND"); ok {
		cfg.Restic.SFTPCommand = v
	}
	if v, ok := os.LookupEnv("SERVERVAULT_RESTIC_TAGS"); ok {
		cfg.Restic.Tags = splitTags(v)
	}

	if v, ok := os.LookupEnv("SERVERVAULT_POSTGRES_ENABLED"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("SERVERVAULT_POSTGRES_ENABLED: %w", err)
		}
		cfg.Postgres.Enabled = b
	}
	if v, ok := os.LookupEnv("SERVERVAULT_POSTGRES_DATABASE"); ok {
		cfg.Postgres.Database = v
	}
	if v, ok := os.LookupEnv("SERVERVAULT_POSTGRES_USER"); ok {
		cfg.Postgres.User = v
	}
	if v, ok := os.LookupEnv("SERVERVAULT_POSTGRES_HOST"); ok {
		cfg.Postgres.Host = v
	}
	if v, ok := os.LookupEnv("SERVERVAULT_POSTGRES_PORT"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("SERVERVAULT_POSTGRES_PORT: %w", err)
		}
		cfg.Postgres.Port = n
	}
	if v, ok := os.LookupEnv("SERVERVAULT_POSTGRES_ZSTD_LEVEL"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("SERVERVAULT_POSTGRES_ZSTD_LEVEL: %w", err)
		}
		cfg.Postgres.ZstdLevel = n
	}

	if v, ok := os.LookupEnv("SERVERVAULT_BACKUP_PATHS"); ok {
		cfg.Backup.Paths = splitPaths(v)
	}
	if v, ok := os.LookupEnv("SERVERVAULT_BACKUP_EXCLUDE_FILE"); ok {
		cfg.Backup.ExcludeFile = v
	}
	if v, ok := os.LookupEnv("SERVERVAULT_BACKUP_ROOT"); ok {
		cfg.Backup.Root = v
	}
	if v, ok := os.LookupEnv("SERVERVAULT_BACKUP_LOCK_FILE"); ok {
		cfg.Backup.LockFile = v
	}

	if v, ok := os.LookupEnv("SERVERVAULT_RESTORE_STAGING_ROOT"); ok {
		cfg.Restore.StagingRoot = v
	}
	if v, ok := os.LookupEnv("SERVERVAULT_RESTORE_TEMP_DATABASE_PREFIX"); ok {
		cfg.Restore.TempDatabasePrefix = v
	}

	if v, ok := os.LookupEnv("SERVERVAULT_RETENTION_KEEP_DAILY"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("SERVERVAULT_RETENTION_KEEP_DAILY: %w", err)
		}
		cfg.Retention.KeepDaily = n
	}
	if v, ok := os.LookupEnv("SERVERVAULT_RETENTION_KEEP_WEEKLY"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("SERVERVAULT_RETENTION_KEEP_WEEKLY: %w", err)
		}
		cfg.Retention.KeepWeekly = n
	}
	if v, ok := os.LookupEnv("SERVERVAULT_RETENTION_KEEP_MONTHLY"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("SERVERVAULT_RETENTION_KEEP_MONTHLY: %w", err)
		}
		cfg.Retention.KeepMonthly = n
	}

	if v, ok := os.LookupEnv("SERVERVAULT_HOST_TAG"); ok {
		cfg.HostTag = v
	}

	if v, ok := os.LookupEnv("SERVERVAULT_NOTIFY_ENABLED"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("SERVERVAULT_NOTIFY_ENABLED: %w", err)
		}
		cfg.Notify.Enabled = b
	}
	if v, ok := os.LookupEnv("SERVERVAULT_NOTIFY_WEBHOOK_URL"); ok {
		cfg.Notify.WebhookURL = v
	}

	return nil
}

// splitTags parses a comma- or whitespace-separated environment variable
// value into a slice of Restic tags, dropping empty elements. Tags are
// simple identifiers (see configs/servervault.example.yaml), so splitting
// on whitespace in addition to commas is safe and matches the shell
// implementation's word-splitting behavior for space-separated lists.
func splitTags(v string) []string {
	fields := strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ' '
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// splitPaths parses a comma-separated environment variable value into a
// slice of filesystem paths. Unlike splitTags, it never splits on
// whitespace: a path may legitimately contain a space (e.g.
// "/var/www/My Site"), so only a literal comma separates entries.
// Leading/trailing whitespace around each entry is trimmed; interior
// whitespace is preserved untouched.
func splitPaths(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	fields := strings.Split(v, ",")
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
