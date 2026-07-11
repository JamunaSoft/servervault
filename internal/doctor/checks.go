package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/JamunaSoft/servervault/internal/config"
)

func checkPlatform() Check {
	detail := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS != "linux" {
		return Check{
			Name:   "OS/architecture",
			Status: StatusWarn,
			Detail: detail + " (ServerVault targets Linux; other platforms are untested)",
		}
	}
	return Check{Name: "OS/architecture", Status: StatusOK, Detail: detail}
}

func checkRequiredCommands(opts Options) Check {
	required := []string{"restic", "zstd"}
	if opts.Config.Postgres.Enabled {
		required = append(required, "pg_dump", "pg_restore")
	}
	if needsSSH(opts.Config.Restic) {
		required = append(required, "ssh")
	}

	var missing []string
	details := make([]string, 0, len(required))
	for _, name := range required {
		path, err := opts.Commands.LookPath(name)
		if err != nil {
			missing = append(missing, name)
			details = append(details, name+": not found")
			continue
		}
		details = append(details, name+": "+path)
	}

	status := StatusOK
	if len(missing) > 0 {
		status = StatusFail
	}
	return Check{
		Name:   "required commands",
		Status: status,
		Detail: strings.Join(details, "; "),
	}
}

func needsSSH(r config.ResticConfig) bool {
	return strings.HasPrefix(r.Repository, "sftp:") || r.SFTPCommand != ""
}

func checkConfigValidation(opts Options) Check {
	errs := config.Validate(opts.Config)
	if len(errs) == 0 {
		return Check{Name: "config validation", Status: StatusOK, Detail: "no issues found"}
	}
	return Check{Name: "config validation", Status: StatusFail, Detail: errs.Error()}
}

func checkSecretPermissions(opts Options) Check {
	const name = "secret permissions"

	path := opts.Config.Restic.PasswordFile
	if path == "" {
		return Check{Name: name, Status: StatusFail, Detail: "restic.password_file is not configured"}
	}

	info, err := os.Stat(path)
	if err != nil {
		return Check{Name: name, Status: StatusFail, Detail: fmt.Sprintf("%s: %v", path, err)}
	}

	if info.Mode().Perm()&0o077 != 0 {
		return Check{
			Name:   name,
			Status: StatusFail,
			Detail: fmt.Sprintf("%s: mode %s is readable by group or other; expected 0600 or stricter", path, info.Mode().Perm()),
		}
	}

	return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("%s: mode %s", path, info.Mode().Perm())}
}

func checkBackupPaths(opts Options) Check {
	const name = "backup paths"

	paths := opts.Config.Backup.Paths
	if len(paths) == 0 {
		return Check{Name: name, Status: StatusWarn, Detail: "no backup paths configured"}
	}

	var missing []string
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return Check{Name: name, Status: StatusFail, Detail: "missing or unreadable: " + strings.Join(missing, ", ")}
	}

	return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("%d path(s) present", len(paths))}
}

// checkRestoreStagingOverlap re-runs config.PathsOverlap's comparison, but
// against symlink-resolved real paths instead of the raw configured
// strings. This catches the case config.Validate structurally cannot: a
// symlink making two logically-distinct configured paths physically the
// same location on disk (e.g. restore.staging_root is a symlink into a
// directory a backup.paths entry also resolves to).
//
// It only runs "where practical" — a configured path that doesn't exist
// yet can't be resolved, so this check reports StatusSkip rather than a
// false pass or fail in that case; config.Validate and checkBackupPaths
// already cover the string-level and existence checks respectively.
func checkRestoreStagingOverlap(opts Options) Check {
	const name = "restore staging overlap (realpath)"

	staging := opts.Config.Restore.StagingRoot
	if staging == "" {
		return Check{Name: name, Status: StatusSkip, Detail: "restore.staging_root is not configured"}
	}

	stagingReal, err := realPath(staging)
	if err != nil {
		return Check{Name: name, Status: StatusSkip, Detail: fmt.Sprintf("cannot resolve %s yet: %v", staging, err)}
	}

	var overlaps []string
	var unresolved int
	for _, bp := range opts.Config.Backup.Paths {
		bpReal, err := realPath(bp)
		if err != nil {
			// Doesn't exist yet or isn't resolvable — checkBackupPaths
			// already reports that; nothing new to say here.
			unresolved++
			continue
		}
		if config.PathsOverlap(stagingReal, bpReal) {
			overlaps = append(overlaps, bp+" -> "+bpReal)
		}
	}

	if len(overlaps) > 0 {
		return Check{
			Name:   name,
			Status: StatusFail,
			Detail: fmt.Sprintf("%s resolves to %s, which overlaps: %s", staging, stagingReal, strings.Join(overlaps, "; ")),
		}
	}

	detail := fmt.Sprintf("%s resolves to %s, no overlap with resolved backup paths", staging, stagingReal)
	if unresolved > 0 {
		detail += fmt.Sprintf(" (%d backup path(s) could not be resolved yet)", unresolved)
	}
	return Check{Name: name, Status: StatusOK, Detail: detail}
}

// realPath resolves p to an absolute, symlink-free path. It requires p to
// exist.
func realPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

const minFreeBytes = 1 << 30 // 1 GiB

func checkDiskSpace(opts Options) Check {
	const name = "local disk space"

	path := opts.Config.Backup.Root
	if path == "" {
		return Check{Name: name, Status: StatusSkip, Detail: "backup.root is not configured"}
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Check{Name: name, Status: StatusWarn, Detail: path + " does not exist yet (will be created on first backup)"}
	}

	free, err := opts.FreeBytes(path)
	if err != nil {
		return Check{Name: name, Status: StatusSkip, Detail: err.Error()}
	}

	detail := fmt.Sprintf("%s: %.1f GiB free", path, float64(free)/(1<<30))
	if free < minFreeBytes {
		return Check{Name: name, Status: StatusFail, Detail: detail}
	}
	return Check{Name: name, Status: StatusOK, Detail: detail}
}

func checkTimezone() Check {
	name, offset := time.Now().Zone()

	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	hours := offset / 3600
	minutes := (offset % 3600) / 60

	return Check{
		Name:   "timezone",
		Status: StatusOK,
		Detail: fmt.Sprintf("%s (UTC%s%02d:%02d)", name, sign, hours, minutes),
	}
}
