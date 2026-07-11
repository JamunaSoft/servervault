package restic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// BackupOptions configures Backup.
type BackupOptions struct {
	// Paths are backed up as given -- may include a single file, such as
	// a compressed PostgreSQL dump alongside filesystem directories.
	Paths       []string
	ExcludeFile string
	Tags        []string
	HostTag     string
}

// Summary is the outcome of a successful (or partially-successful, in the
// ExitBackupIncomplete sense) backup.
type Summary struct {
	SnapshotID   string
	FilesNew     int
	FilesChanged int
	BytesAdded   int64
	// Warnings holds per-file errors restic reported during the backup
	// (e.g. "permission denied" on one file) when the overall backup
	// still produced a snapshot (restic exit code 3). An empty backup
	// with no warnings and no error is a clean success.
	Warnings []string
}

// Backup runs `restic backup --json`, returning a Summary parsed from
// restic's structured output. It never runs `forget`/`prune` -- retention
// is a separate concern (internal/retention, not yet implemented).
//
// restic's exit code 3 ("some source files could not be read") is treated
// as a successful backup with Summary.Warnings populated, not a Go error:
// it's common (a file vanishing mid-read, a permission-denied on a socket
// file) and restic still produces a usable snapshot. Every other non-zero
// exit is a hard failure.
func (r *Repository) Backup(ctx context.Context, opts BackupOptions) (Summary, error) {
	args := []string{"backup", "--json"}
	if opts.HostTag != "" {
		args = append(args, "--host", opts.HostTag)
	}
	for _, tag := range opts.Tags {
		args = append(args, "--tag", tag)
	}
	if opts.ExcludeFile != "" {
		args = append(args, "--exclude-file", opts.ExcludeFile)
	}
	args = append(args, opts.Paths...)

	stdout, stderr, runErr := r.run(ctx, args)

	code := classify(runErr)
	if runErr != nil && code != ExitBackupIncomplete {
		return Summary{}, &ExitError{Code: code, Err: wrapWithStderr(runErr, "restic backup", stderr)}
	}

	summary, err := parseBackupJSON(stdout.Bytes())
	if err != nil {
		if runErr != nil {
			// restic itself reported a problem and we can't even parse
			// its output -- surface the original failure, it's more
			// actionable than a JSON parse error.
			return Summary{}, &ExitError{Code: code, Err: wrapWithStderr(runErr, "restic backup", stderr)}
		}
		return Summary{}, fmt.Errorf("restic backup: parse output: %w", err)
	}
	return summary, nil
}

type jsonEnvelope struct {
	MessageType string `json:"message_type"`
}

type jsonSummary struct {
	FilesNew     int    `json:"files_new"`
	FilesChanged int    `json:"files_changed"`
	DataAdded    int64  `json:"data_added"`
	SnapshotID   string `json:"snapshot_id"`
}

type jsonBackupError struct {
	Item  string `json:"item"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// parseBackupJSON scans restic's `--json` backup output (one JSON object
// per line) for the terminal "summary" event and any "error" events along
// the way.
func parseBackupJSON(output []byte) (Summary, error) {
	var summary Summary
	var found bool

	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var env jsonEnvelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue // tolerate a stray non-JSON line rather than fail
		}

		switch env.MessageType {
		case "summary":
			var s jsonSummary
			if err := json.Unmarshal(line, &s); err != nil {
				return Summary{}, fmt.Errorf("parse summary line: %w", err)
			}
			summary.SnapshotID = s.SnapshotID
			summary.FilesNew = s.FilesNew
			summary.FilesChanged = s.FilesChanged
			summary.BytesAdded = s.DataAdded
			found = true
		case "error":
			var e jsonBackupError
			if err := json.Unmarshal(line, &e); err == nil {
				summary.Warnings = append(summary.Warnings, fmt.Sprintf("%s: %s", e.Item, e.Error.Message))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Summary{}, fmt.Errorf("scan output: %w", err)
	}
	if !found {
		return Summary{}, errors.New("no summary event found in restic backup output")
	}
	return summary, nil
}

// SnapshotsOptions filters Snapshots.
type SnapshotsOptions struct {
	Host   string
	Tags   []string
	Latest int // 0 = no limit
}

// Snapshot is one entry from `restic snapshots --json`.
type Snapshot struct {
	ID       string
	Time     time.Time
	Hostname string
	Tags     []string
	Paths    []string
}
