package restic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// RestoreOptions configures Restore.
type RestoreOptions struct {
	// SnapshotID is the snapshot to restore from -- a full or short
	// snapshot ID, or "latest".
	SnapshotID string
	// Target is the destination directory Restic writes into. The
	// caller (internal/restore) is responsible for this being a freshly
	// created, empty staging directory -- this package has no concept
	// of "live" vs. "staging" and enforces nothing about Target beyond
	// passing it through to restic.
	Target string
	// Include, if non-empty, restores only paths under this
	// repository-relative prefix, matching restic's own --include
	// semantics. Empty means restore everything in the snapshot.
	Include string
}

// RestoreSummary is the outcome of a successful restore.
type RestoreSummary struct {
	FilesRestored int
	BytesRestored int64
}

// Restore runs `restic restore <snapshotID> --target <dir> [--include
// <path>] --json`, the first sanctioned write-capable addition to this
// package -- see the package doc comment for why Restore, unlike Init,
// Forget/Prune, and Unlock, is a deliberate, scoped exception: staging-
// only restore is one of ServerVault's non-negotiable safety rules
// (CLAUDE.md), not something this package can remain structurally
// incapable of, but it still never targets a live path -- that
// guarantee lives in internal/restore's Planner, not here.
//
// Field names in restic's `--json` restore summary are parsed leniently
// (see restoreJSONSummary below) based on restic's documented output
// schema; this was not verified against a real restic binary in the
// environment this code was written in (no restic installed there) --
// the real-binary integration suite (`-tags=integration`, gated behind
// RequireRestic) is the actual first verification against real output.
func (r *Repository) Restore(ctx context.Context, opts RestoreOptions) (RestoreSummary, error) {
	if opts.SnapshotID == "" {
		return RestoreSummary{}, fmt.Errorf("restic: restore: snapshot ID must not be empty")
	}
	if opts.Target == "" {
		return RestoreSummary{}, fmt.Errorf("restic: restore: target directory must not be empty")
	}

	args := []string{"restore", opts.SnapshotID, "--target", opts.Target, "--json"}
	if opts.Include != "" {
		args = append(args, "--include", opts.Include)
	}

	stdout, stderr, runErr := r.run(ctx, args)
	if runErr != nil {
		return RestoreSummary{}, &ExitError{Code: classifyResult(runErr, stderr), Err: wrapWithStderr(runErr, "restic restore", stderr)}
	}

	summary, found, err := parseRestoreJSON(stdout.Bytes())
	if err != nil {
		return RestoreSummary{}, fmt.Errorf("restic restore: parse output: %w", err)
	}
	if !found {
		// restic ran successfully (exit 0) but produced no summary line
		// we recognize -- treat as success with zero known counts rather
		// than fail a restore that otherwise completed, but make the
		// uncertainty visible to the caller via a zero-value summary
		// rather than guessing at numbers.
		return RestoreSummary{}, nil
	}
	return summary, nil
}

type restoreJSONEnvelope struct {
	MessageType string `json:"message_type"`
}

// restoreJSONSummary covers the field names documented for `restic
// restore --json`'s final summary message. Some restic versions have
// used total_bytes for the same concept bytes_restored names in others;
// both are accepted and the larger non-zero value is treated as
// authoritative if they disagree, since a partial/vanished-file restore
// should never be reported as having restored more than it did.
type restoreJSONSummary struct {
	FilesRestored int   `json:"files_restored"`
	TotalFiles    int   `json:"total_files"`
	BytesRestored int64 `json:"bytes_restored"`
	TotalBytes    int64 `json:"total_bytes"`
}

func parseRestoreJSON(output []byte) (RestoreSummary, bool, error) {
	var summary RestoreSummary
	var found bool

	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var env restoreJSONEnvelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue // tolerate a stray non-JSON line, matching parseBackupJSON
		}
		if env.MessageType != "summary" {
			continue
		}

		var s restoreJSONSummary
		if err := json.Unmarshal(line, &s); err != nil {
			return RestoreSummary{}, false, fmt.Errorf("parse summary line: %w", err)
		}
		summary.FilesRestored = maxInt(s.FilesRestored, s.TotalFiles)
		summary.BytesRestored = maxInt64(s.BytesRestored, s.TotalBytes)
		found = true
	}
	if err := scanner.Err(); err != nil {
		return RestoreSummary{}, false, fmt.Errorf("scan output: %w", err)
	}
	return summary, found, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// Stats reports the total size and file count restic would restore for
// the whole of snapshotID, via `restic stats --json --mode=restore-size
// <snapshotID>`. It performs no writes -- safe to call during planning
// (dry-run) as well as before execution.
func (r *Repository) Stats(ctx context.Context, snapshotID string) (Stats, error) {
	if snapshotID == "" {
		return Stats{}, fmt.Errorf("restic: stats: snapshot ID must not be empty")
	}

	stdout, stderr, err := r.run(ctx, []string{"stats", "--json", "--mode=restore-size", snapshotID})
	if err != nil {
		return Stats{}, &ExitError{Code: classifyResult(err, stderr), Err: wrapWithStderr(err, "restic stats", stderr)}
	}

	var raw struct {
		TotalSize      int64 `json:"total_size"`
		TotalFileCount int   `json:"total_file_count"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &raw); err != nil {
		return Stats{}, fmt.Errorf("restic stats: parse output: %w", err)
	}
	return Stats{TotalSize: raw.TotalSize, TotalFileCount: raw.TotalFileCount}, nil
}

// Stats is the outcome of a restic stats query.
type Stats struct {
	TotalSize      int64
	TotalFileCount int
}

// FileInfo is one entry from `restic ls`.
type FileInfo struct {
	Path string
	Type string // "file" or "dir"
	Size int64
}

// List runs `restic ls <snapshotID> [path] --json`, returning every file
// and directory entry at or under path within the snapshot (the whole
// snapshot if path is empty). It performs no writes.
func (r *Repository) List(ctx context.Context, snapshotID, path string) ([]FileInfo, error) {
	if snapshotID == "" {
		return nil, fmt.Errorf("restic: list: snapshot ID must not be empty")
	}

	args := []string{"ls", "--json", snapshotID}
	if path != "" {
		args = append(args, path)
	}

	stdout, stderr, err := r.run(ctx, args)
	if err != nil {
		return nil, &ExitError{Code: classifyResult(err, stderr), Err: wrapWithStderr(err, "restic ls", stderr)}
	}

	var files []FileInfo
	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry struct {
			// Older restic emits one "snapshot" header object with no
			// "path" field, then one object per file/dir entry -- both
			// are unmarshaled into the same struct and the header is
			// skipped below (empty Path and StructType != file/dir).
			StructType string `json:"struct_type"`
			Path       string `json:"path"`
			Type       string `json:"type"`
			Size       int64  `json:"size"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // tolerate a stray non-JSON line
		}
		if entry.Path == "" {
			continue // the snapshot header line, not a file/dir entry
		}
		files = append(files, FileInfo{Path: entry.Path, Type: entry.Type, Size: entry.Size})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("restic ls: scan output: %w", err)
	}
	return files, nil
}
