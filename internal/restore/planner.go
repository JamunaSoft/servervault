package restore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/JamunaSoft/servervault/internal/config"
)

// Planner builds Plans from real repository metadata. It performs no
// writes -- every method it calls on ResticClient (Stats, List) is
// read-only, so calling Plan is always safe, including for a dry run.
type Planner struct {
	restic ResticClient
	cfg    *config.Config

	// now and randSuffix are overridden in tests for deterministic
	// output; production callers get NewPlanner's real-clock,
	// real-random defaults.
	now        func() time.Time
	randSuffix func() (string, error)
}

// NewPlanner builds a Planner. cfg must be non-nil and already validated
// (see config.Validate) -- Planner does not re-validate configuration
// structure, only the plan-specific safety checks documented on each
// planning method.
func NewPlanner(resticClient ResticClient, cfg *config.Config) (*Planner, error) {
	if resticClient == nil {
		return nil, fmt.Errorf("restore: planner: restic client must not be nil")
	}
	if cfg == nil {
		return nil, fmt.Errorf("restore: planner: config must not be nil")
	}
	return &Planner{
		restic:     resticClient,
		cfg:        cfg,
		now:        func() time.Time { return time.Now().UTC() },
		randSuffix: randomHexSuffix,
	}, nil
}

func randomHexSuffix() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("restore: generate random suffix: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Plan builds an immutable Plan for opts. It performs no writes.
func (p *Planner) Plan(ctx context.Context, opts PlanOptions) (Plan, error) {
	if opts.SnapshotID == "" {
		return Plan{}, fmt.Errorf("restore: plan: snapshot ID must not be empty")
	}

	switch opts.Target {
	case TargetFiles:
		return p.planFiles(ctx, opts)
	case TargetTempDB:
		return p.planTempDB(ctx, opts)
	default:
		return Plan{}, fmt.Errorf("restore: plan: unknown target %q (want %q or %q)", opts.Target, TargetFiles, TargetTempDB)
	}
}

func (p *Planner) planFiles(ctx context.Context, opts PlanOptions) (Plan, error) {
	suffix, err := p.randSuffix()
	if err != nil {
		return Plan{}, err
	}
	generatedAt := p.now()
	destination := freshStagingPath(p.cfg.Restore.StagingRoot, "files", shortID(opts.SnapshotID), generatedAt, suffix)

	safetyChecks := []string{
		"destination is a newly generated path under restore.staging_root, never a configured live path",
	}
	for _, bp := range p.cfg.Backup.Paths {
		if config.PathsOverlap(destination, bp) {
			// Should be unreachable given destination is freshly
			// generated under a staging_root that config.Validate
			// already guarantees does not overlap any backup path --
			// checked anyway as defense in depth, not defensive
			// programming theater: Plan must never silently produce an
			// unsafe destination.
			return Plan{}, fmt.Errorf("restore: plan: generated destination %q unexpectedly overlaps configured backup path %q", destination, bp)
		}
	}
	safetyChecks = append(safetyChecks, "destination does not overlap any configured backup.paths entry")

	if path.Clean(destination) == "/" {
		return Plan{}, fmt.Errorf("restore: plan: generated destination resolved to the root filesystem, refusing to proceed")
	}
	safetyChecks = append(safetyChecks, "destination is not the root filesystem (/)")

	// Always resolved via `restic ls`, never `restic stats`: stats has a
	// documented "if no snapshot is given, this command runs against
	// all snapshots" fallback that -- for at least some restic
	// versions -- also triggers when the given snapshot ID simply
	// doesn't resolve to anything, rather than failing outright. That
	// turned a bogus snapshot ID into a silently-succeeding Plan built
	// from the wrong snapshot's stats instead of the requested one --
	// caught by TestIntegration_Restore_Files_InvalidSnapshotID against
	// real restic in CI, not reproducible locally (no restic binary in
	// this environment). `restic ls` has no such fallback: it must
	// resolve to exactly one real snapshot or it fails, which is why
	// the scoped-path branch below was never affected by this bug.
	var expectedFiles, expectedBytes int64
	entries, err := p.restic.List(ctx, opts.SnapshotID, opts.Path)
	if err != nil {
		return Plan{}, fmt.Errorf("restore: plan: list snapshot %q: %w", opts.SnapshotID, err)
	}
	if len(entries) == 0 {
		if opts.Path != "" {
			return Plan{}, ErrSnapshotPathNotFound
		}
		return Plan{}, ErrSnapshotNotFound
	}
	for _, e := range entries {
		if e.Type == "file" {
			expectedFiles++
			expectedBytes += e.Size
		}
	}

	return Plan{
		SnapshotID:       opts.SnapshotID,
		Target:           TargetFiles,
		RepositoryPath:   opts.Path,
		Destination:      destination,
		ExpectedFiles:    expectedFiles,
		ExpectedBytes:    expectedBytes,
		BytesKnown:       true,
		RequiredCommands: []string{"restic"},
		SafetyChecks:     safetyChecks,
		GeneratedAt:      generatedAt,
	}, nil
}

func (p *Planner) planTempDB(ctx context.Context, opts PlanOptions) (Plan, error) {
	if !p.cfg.Postgres.Enabled {
		return Plan{}, ErrDatabaseDisabled
	}
	if opts.Database != "" && opts.Database != p.cfg.Postgres.Database {
		return Plan{}, &ErrUnknownDatabase{Requested: opts.Database}
	}

	dumpDir := filepath.Join(p.cfg.Backup.Root, "postgresql")
	entries, err := p.restic.List(ctx, opts.SnapshotID, dumpDir)
	if err != nil {
		return Plan{}, fmt.Errorf("restore: plan: list snapshot dump directory %q: %w", dumpDir, err)
	}

	var dumps []struct {
		path string
		size int64
	}
	for _, e := range entries {
		if e.Type == "file" && strings.HasSuffix(e.Path, ".dump.zst") {
			dumps = append(dumps, struct {
				path string
				size int64
			}{e.Path, e.Size})
		}
	}
	switch len(dumps) {
	case 0:
		return Plan{}, ErrDumpNotFound
	case 1:
		// exactly one -- proceed
	default:
		return Plan{}, ErrAmbiguousDump
	}
	dump := dumps[0]

	suffix, err := p.randSuffix()
	if err != nil {
		return Plan{}, err
	}
	generatedAt := p.now()

	tempDatabaseName := p.cfg.Restore.TempDatabasePrefix + shortID(opts.SnapshotID) + "_" + suffix
	if tempDatabaseName == p.cfg.Postgres.Database {
		// Unreachable given TempDatabasePrefix != Postgres.Database is
		// enforced by config.Validate, and this name additionally has a
		// snapshot-ID/random suffix appended -- checked anyway, per the
		// same defense-in-depth reasoning as planFiles.
		return Plan{}, fmt.Errorf("restore: plan: generated temporary database name unexpectedly equals the live database name")
	}

	destination := freshStagingPath(p.cfg.Restore.StagingRoot, "db", shortID(opts.SnapshotID), generatedAt, suffix)

	return Plan{
		SnapshotID:       opts.SnapshotID,
		Target:           TargetTempDB,
		RepositoryPath:   dump.path,
		Destination:      destination,
		TempDatabaseName: tempDatabaseName,
		DatabaseName:     p.cfg.Postgres.Database,
		ExpectedFiles:    1,
		ExpectedBytes:    dump.size,
		BytesKnown:       true,
		RequiredCommands: []string{"restic", "zstd", "pg_restore"},
		SafetyChecks: []string{
			"temporary database name is newly generated and does not equal postgres.database",
			"exactly one PostgreSQL dump file was found in the snapshot -- refuses to guess among multiple",
		},
		GeneratedAt: generatedAt,
	}, nil
}

// freshStagingPath builds a unique, timestamped destination path under
// root. kind and shortSnapshotID are cosmetic (make the directory name
// legible to an operator browsing restore.staging_root) -- uniqueness
// comes entirely from the timestamp plus the random suffix, not from
// them.
func freshStagingPath(root, kind, shortSnapshotID string, at time.Time, suffix string) string {
	name := fmt.Sprintf("restore-%s-%s-%s-%s", kind, shortSnapshotID, at.Format("20060102-150405"), suffix)
	return filepath.Join(root, name)
}

func shortID(snapshotID string) string {
	if len(snapshotID) > 8 {
		return snapshotID[:8]
	}
	if snapshotID == "" {
		return "unknown"
	}
	return snapshotID
}
