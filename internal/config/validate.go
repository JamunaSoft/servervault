package config

import (
	"path"
	"strings"
)

// ValidationError describes one invalid field found by Validate.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// ValidationErrors is a non-empty list of ValidationError, satisfying the
// error interface so callers that just want a single error can use it
// directly.
type ValidationErrors []ValidationError

func (errs ValidationErrors) Error() string {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "; ")
}

// resticBackendPrefixes lists the repository URL prefixes ServerVault
// recognizes. This intentionally mirrors what Restic itself supports for
// the backends named in docs/security-model.md and the platform proposal:
// SFTP (including Hetzner Storage Box), S3-compatible storage, and
// Backblaze B2. A bare absolute path is also accepted for local/testing
// repositories.
var resticBackendPrefixes = []string{"sftp:", "s3:", "b2:", "rest:", "local:"}

// Validate performs structural, filesystem-free validation of cfg: are the
// required fields present, do paths look sane, are values in range. It does
// not touch the filesystem or network — checks that require I/O (does the
// password file exist, is it readable, is PostgreSQL reachable) belong to
// internal/doctor, which validates the deployed environment rather than the
// configuration's shape.
//
// Validate returns every problem found, not just the first, so `servervault
// config validate` can report a complete list in one run.
func Validate(cfg *Config) ValidationErrors {
	var errs ValidationErrors

	errs = append(errs, validateRestic(cfg.Restic)...)
	errs = append(errs, validateBackup(cfg.Backup)...)
	errs = append(errs, validateRetention(cfg.Retention)...)
	errs = append(errs, validatePostgres(cfg.Postgres)...)
	errs = append(errs, validateRestore(cfg.Restore, cfg.Postgres, cfg.Backup)...)
	errs = append(errs, validateStateDir(cfg.StateDir)...)

	return errs
}

func validateStateDir(dir string) ValidationErrors {
	var errs ValidationErrors
	if dir == "" {
		errs = append(errs, ValidationError{"state_dir", "must not be empty"})
	} else if !strings.HasPrefix(dir, "/") {
		errs = append(errs, ValidationError{"state_dir", "must be an absolute path"})
	}
	return errs
}

func validateRestic(r ResticConfig) ValidationErrors {
	var errs ValidationErrors

	if r.Repository == "" {
		errs = append(errs, ValidationError{"restic.repository", "must not be empty"})
	} else if !hasResticBackendPrefix(r.Repository) {
		errs = append(errs, ValidationError{
			"restic.repository",
			"unrecognized backend syntax (expected one of sftp:, s3:, b2:, rest:, local:, or an absolute path)",
		})
	}

	if r.PasswordFile == "" {
		errs = append(errs, ValidationError{"restic.password_file", "must not be empty"})
	} else if !strings.HasPrefix(r.PasswordFile, "/") {
		errs = append(errs, ValidationError{"restic.password_file", "must be an absolute path"})
	}

	return errs
}

func hasResticBackendPrefix(repository string) bool {
	for _, prefix := range resticBackendPrefixes {
		if strings.HasPrefix(repository, prefix) {
			return true
		}
	}
	return strings.HasPrefix(repository, "/")
}

func validateBackup(b BackupConfig) ValidationErrors {
	var errs ValidationErrors

	if len(b.Paths) == 0 {
		errs = append(errs, ValidationError{"backup.paths", "must list at least one path"})
	}
	for _, p := range b.Paths {
		if !strings.HasPrefix(p, "/") {
			errs = append(errs, ValidationError{"backup.paths", "path " + p + " must be absolute"})
		}
	}

	if b.Root == "" {
		errs = append(errs, ValidationError{"backup.root", "must not be empty"})
	} else if !strings.HasPrefix(b.Root, "/") {
		errs = append(errs, ValidationError{"backup.root", "must be an absolute path"})
	}

	if b.LockFile == "" {
		errs = append(errs, ValidationError{"backup.lock_file", "must not be empty"})
	} else if !strings.HasPrefix(b.LockFile, "/") {
		errs = append(errs, ValidationError{"backup.lock_file", "must be an absolute path"})
	}

	return errs
}

func validateRetention(r RetentionConfig) ValidationErrors {
	var errs ValidationErrors

	if r.KeepDaily < 0 {
		errs = append(errs, ValidationError{"retention.keep_daily", "must not be negative"})
	}
	if r.KeepWeekly < 0 {
		errs = append(errs, ValidationError{"retention.keep_weekly", "must not be negative"})
	}
	if r.KeepMonthly < 0 {
		errs = append(errs, ValidationError{"retention.keep_monthly", "must not be negative"})
	}
	if r.KeepDaily == 0 && r.KeepWeekly == 0 && r.KeepMonthly == 0 {
		errs = append(errs, ValidationError{
			"retention",
			"keep_daily, keep_weekly, and keep_monthly are all 0 — every snapshot would be pruned",
		})
	}

	// A hard floor of 1, regardless of the configured value: retention
	// must never be able to prune a repository to zero snapshots. This
	// is intentionally not configurable down to 0 -- see RetentionConfig's
	// doc comment.
	if r.MinKeepTotal < 1 {
		errs = append(errs, ValidationError{"retention.min_keep_total", "must be at least 1"})
	}

	// No "unlimited" value on purpose -- see RetentionConfig's doc
	// comment. A misconfigured 0 here would silently disable the one
	// safety limit that catches a catastrophic keep-policy mistake.
	if r.MaxDeleteCount <= 0 {
		errs = append(errs, ValidationError{"retention.max_delete_count", "must be greater than 0"})
	}

	if r.LockFile == "" {
		errs = append(errs, ValidationError{"retention.lock_file", "must not be empty"})
	} else if !strings.HasPrefix(r.LockFile, "/") {
		errs = append(errs, ValidationError{"retention.lock_file", "must be an absolute path"})
	}

	return errs
}

func validatePostgres(p PostgresConfig) ValidationErrors {
	var errs ValidationErrors

	if !p.Enabled {
		return errs
	}

	if p.Database == "" {
		errs = append(errs, ValidationError{"postgres.database", "must not be empty when postgres.enabled is true"})
	}
	if p.User == "" {
		errs = append(errs, ValidationError{"postgres.user", "must not be empty when postgres.enabled is true"})
	}
	if p.Port <= 0 || p.Port > 65535 {
		errs = append(errs, ValidationError{"postgres.port", "must be between 1 and 65535"})
	}
	if p.ZstdLevel < 1 || p.ZstdLevel > 19 {
		errs = append(errs, ValidationError{"postgres.zstd_level", "must be between 1 and 19"})
	}

	return errs
}

// validateRestore checks the restore destinations rules from
// docs/security-model.md: restores must never be configured to land on,
// inside, or around a live backup path, or in the live database.
func validateRestore(r RestoreConfig, pg PostgresConfig, b BackupConfig) ValidationErrors {
	var errs ValidationErrors

	switch {
	case r.StagingRoot == "":
		errs = append(errs, ValidationError{"restore.staging_root", "must not be empty"})
	case !strings.HasPrefix(r.StagingRoot, "/"):
		errs = append(errs, ValidationError{"restore.staging_root", "must be an absolute path"})
	case path.Clean(r.StagingRoot) == "/":
		errs = append(errs, ValidationError{"restore.staging_root", "must not be the root filesystem (/)"})
	default:
		for _, bp := range b.Paths {
			if PathsOverlap(r.StagingRoot, bp) {
				errs = append(errs, ValidationError{
					"restore.staging_root",
					"overlaps a live backup.paths entry (" + bp + ") — restores must land in staging, never inside or containing a live path",
				})
			}
		}
	}

	if r.TempDatabasePrefix == "" {
		errs = append(errs, ValidationError{"restore.temp_database_prefix", "must not be empty"})
	} else if pg.Enabled && r.TempDatabasePrefix == pg.Database {
		errs = append(errs, ValidationError{
			"restore.temp_database_prefix",
			"must not equal postgres.database — restores must land in a temporary database, not the live one",
		})
	}

	if r.LockFile == "" {
		errs = append(errs, ValidationError{"restore.lock_file", "must not be empty"})
	} else if !strings.HasPrefix(r.LockFile, "/") {
		errs = append(errs, ValidationError{"restore.lock_file", "must be an absolute path"})
	}

	return errs
}

// PathsOverlap reports whether cleaned absolute paths a and b are equal, or
// one is nested inside the other, using path-segment-aware comparison —
// so a deceptive prefix like /var/www-old is never mistaken for being
// inside /var/www, and a trailing slash never changes the answer.
//
// This is pure string comparison: no filesystem access, no symlink
// resolution, matching Validate's filesystem-free contract. See
// internal/doctor for the realpath-aware overlap check that runs against
// the actual deployed environment, where a symlink could make two
// logically-different configured paths physically the same.
func PathsOverlap(a, b string) bool {
	a = path.Clean(a)
	b = path.Clean(b)
	if a == b {
		return true
	}
	return isWithin(a, b) || isWithin(b, a)
}

// isWithin reports whether child is nested inside parent. parent == "/" is
// handled as a special case: naively checking strings.HasPrefix(child,
// parent+"/") would build the separator "//" for a root parent, which no
// cleaned path ever contains, so it would wrongly report that nothing is
// ever nested inside "/".
func isWithin(child, parent string) bool {
	if parent == "/" {
		return true
	}
	return strings.HasPrefix(child, parent+"/")
}
