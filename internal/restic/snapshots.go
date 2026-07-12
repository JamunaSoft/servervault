package restic

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Snapshots runs `restic snapshots --json`, optionally filtered by host,
// tags, and a "latest N" limit.
func (r *Repository) Snapshots(ctx context.Context, opts SnapshotsOptions) ([]Snapshot, error) {
	args := []string{"snapshots", "--json"}
	if opts.Host != "" {
		args = append(args, "--host", opts.Host)
	}
	for _, tag := range opts.Tags {
		args = append(args, "--tag", tag)
	}
	if opts.Latest > 0 {
		args = append(args, "--latest", strconv.Itoa(opts.Latest))
	}

	stdout, stderr, err := r.run(ctx, args)
	if err != nil {
		return nil, &ExitError{Code: classifyResult(err, stderr), Err: wrapWithStderr(err, "restic snapshots", stderr)}
	}

	var raw []struct {
		ID       string    `json:"id"`
		Time     time.Time `json:"time"`
		Hostname string    `json:"hostname"`
		Tags     []string  `json:"tags"`
		Paths    []string  `json:"paths"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return nil, fmt.Errorf("restic snapshots: parse output: %w", err)
	}

	snapshots := make([]Snapshot, len(raw))
	for i, s := range raw {
		snapshots[i] = Snapshot{
			ID:       s.ID,
			Time:     s.Time,
			Hostname: s.Hostname,
			Tags:     s.Tags,
			Paths:    s.Paths,
		}
	}
	return snapshots, nil
}
