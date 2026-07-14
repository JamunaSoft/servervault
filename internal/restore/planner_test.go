package restore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/restic"
)

func testRestoreConfig() *config.Config {
	cfg := config.Defaults()
	cfg.Restic.Repository = "local:/tmp/does-not-matter"
	cfg.Backup.Paths = []string{"/var/www"}
	cfg.Backup.Root = "/var/backups/servervault"
	cfg.Postgres.Enabled = true
	cfg.Postgres.Database = "app_production"
	return cfg
}

func deterministicPlanner(t *testing.T, restic ResticClient, cfg *config.Config) *Planner {
	t.Helper()
	p, err := NewPlanner(restic, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	p.now = func() time.Time { return time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC) }
	p.randSuffix = func() (string, error) { return "deadbeef0001", nil }
	return p
}

func TestPlanner_PlanFiles_WholeSnapshot(t *testing.T) {
	// Whole-snapshot stats are derived from `restic ls` (all entries,
	// no path filter), not `restic stats` -- see planFiles' comment on
	// why. 7 file entries plus 2 directory entries (ignored) sums to
	// ExpectedFiles=7, ExpectedBytes=2048.
	fr := &fakeRestic{listFiles: []restic.FileInfo{
		{Path: "/var/www/a", Type: "file", Size: 100},
		{Path: "/var/www/b", Type: "file", Size: 200},
		{Path: "/var/www/c", Type: "file", Size: 300},
		{Path: "/var/www/d", Type: "file", Size: 400},
		{Path: "/var/www/e", Type: "file", Size: 500},
		{Path: "/var/www/f", Type: "file", Size: 548},
		{Path: "/var/www/subdir/g", Type: "file", Size: 0},
		{Path: "/var/www/subdir", Type: "dir"},
		{Path: "/var/www", Type: "dir"},
	}}
	cfg := testRestoreConfig()
	p := deterministicPlanner(t, fr, cfg)

	plan, err := p.Plan(context.Background(), PlanOptions{SnapshotID: "abcdef1234567890", Target: TargetFiles})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if plan.Target != TargetFiles {
		t.Errorf("Target = %s, want %s", plan.Target, TargetFiles)
	}
	if plan.ExpectedFiles != 7 || plan.ExpectedBytes != 2048 || !plan.BytesKnown {
		t.Errorf("plan stats = %+v, want ExpectedFiles=7 ExpectedBytes=2048 BytesKnown=true", plan)
	}
	if plan.Destination == "" {
		t.Error("Destination must not be empty")
	}
	for _, bp := range cfg.Backup.Paths {
		if config.PathsOverlap(plan.Destination, bp) {
			t.Errorf("plan destination %q overlaps configured backup path %q", plan.Destination, bp)
		}
	}
	if len(plan.SafetyChecks) == 0 {
		t.Error("SafetyChecks should be populated")
	}
	if len(plan.RequiredCommands) != 1 || plan.RequiredCommands[0] != "restic" {
		t.Errorf("RequiredCommands = %v, want [restic]", plan.RequiredCommands)
	}
}

func TestPlanner_PlanFiles_WholeSnapshotNotFound(t *testing.T) {
	// Pins the fix for a bug where a nonexistent snapshot ID silently
	// succeeded on whole-snapshot Plan calls (opts.Path == "") because
	// planFiles used to resolve existence via restic stats, which has a
	// documented fallback to "all snapshots" that masked the bad ID. Now
	// that both branches go through List, an empty result set (what a
	// real `restic ls <bogus-id>` returns -- it has no such fallback)
	// must be rejected the same way for both the whole-snapshot and
	// scoped-path cases.
	fr := &fakeRestic{listFiles: nil}
	p := deterministicPlanner(t, fr, testRestoreConfig())

	_, err := p.Plan(context.Background(), PlanOptions{SnapshotID: "0000000000000000000000000000000000000000000000000000000000000000", Target: TargetFiles})
	if !errors.Is(err, ErrSnapshotNotFound) {
		t.Errorf("err = %v, want ErrSnapshotNotFound", err)
	}
}

func TestPlanner_PlanFiles_ScopedPath(t *testing.T) {
	fr := &fakeRestic{listFiles: []restic.FileInfo{
		{Path: "/var/www/app/index.html", Type: "file", Size: 100},
		{Path: "/var/www/app/assets", Type: "dir"},
		{Path: "/var/www/app/assets/logo.png", Type: "file", Size: 500},
	}}
	p := deterministicPlanner(t, fr, testRestoreConfig())

	plan, err := p.Plan(context.Background(), PlanOptions{SnapshotID: "abc123", Target: TargetFiles, Path: "/var/www/app"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.ExpectedFiles != 2 || plan.ExpectedBytes != 600 {
		t.Errorf("plan = %+v, want ExpectedFiles=2 ExpectedBytes=600 (directories excluded)", plan)
	}
	if plan.RepositoryPath != "/var/www/app" {
		t.Errorf("RepositoryPath = %q, want /var/www/app", plan.RepositoryPath)
	}
}

func TestPlanner_PlanFiles_PathNotFound(t *testing.T) {
	fr := &fakeRestic{listFiles: nil}
	p := deterministicPlanner(t, fr, testRestoreConfig())

	_, err := p.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetFiles, Path: "/does/not/exist"})
	if !errors.Is(err, ErrSnapshotPathNotFound) {
		t.Errorf("err = %v, want ErrSnapshotPathNotFound", err)
	}
}

func TestPlanner_Plan_NeverWrites(t *testing.T) {
	// fakeRestic has no filesystem-writing behavior in Stats/List (only
	// Restore does, and Restore is never called by Plan) -- this test
	// exists as a structural guard: if Plan ever grows a call to
	// restic.Restore, this test's fakeRestic (which returns a zero-value
	// RestoreSummary and records nothing useful for this purpose) would
	// need to change, which is a deliberate friction point that makes
	// "Plan performs no writes" hard to violate silently.
	fr := &fakeRestic{listFiles: []restic.FileInfo{{Path: "file1", Type: "file", Size: 10}}}
	p := deterministicPlanner(t, fr, testRestoreConfig())

	if _, err := p.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetFiles}); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if fr.restoreCallCount() != 0 {
		t.Errorf("Plan called Restore %d times, want 0 -- planning must never write", fr.restoreCallCount())
	}
}

func TestPlanner_PlanTempDB_Success(t *testing.T) {
	fr := &fakeRestic{listFiles: []restic.FileInfo{
		{Path: "/var/backups/servervault/postgresql/app_production_20260713.dump.zst", Type: "file", Size: 4096},
	}}
	cfg := testRestoreConfig()
	p := deterministicPlanner(t, fr, cfg)

	plan, err := p.Plan(context.Background(), PlanOptions{SnapshotID: "abc123def456", Target: TargetTempDB})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.TempDatabaseName == cfg.Postgres.Database {
		t.Error("TempDatabaseName must never equal the live database name")
	}
	if plan.TempDatabaseName == "" {
		t.Error("TempDatabaseName must not be empty")
	}
	if plan.RepositoryPath != "/var/backups/servervault/postgresql/app_production_20260713.dump.zst" {
		t.Errorf("RepositoryPath = %q", plan.RepositoryPath)
	}
	if plan.ExpectedFiles != 1 || plan.ExpectedBytes != 4096 {
		t.Errorf("plan = %+v, want ExpectedFiles=1 ExpectedBytes=4096", plan)
	}
	wantCmds := []string{"restic", "zstd", "pg_restore"}
	if len(plan.RequiredCommands) != len(wantCmds) {
		t.Errorf("RequiredCommands = %v, want %v", plan.RequiredCommands, wantCmds)
	}
}

func TestPlanner_PlanTempDB_NoDumpFound(t *testing.T) {
	fr := &fakeRestic{listFiles: nil}
	p := deterministicPlanner(t, fr, testRestoreConfig())

	_, err := p.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetTempDB})
	if !errors.Is(err, ErrDumpNotFound) {
		t.Errorf("err = %v, want ErrDumpNotFound", err)
	}
}

func TestPlanner_PlanTempDB_MultipleDumpsIsAmbiguous(t *testing.T) {
	fr := &fakeRestic{listFiles: []restic.FileInfo{
		{Path: "/var/backups/servervault/postgresql/a.dump.zst", Type: "file", Size: 10},
		{Path: "/var/backups/servervault/postgresql/b.dump.zst", Type: "file", Size: 20},
	}}
	p := deterministicPlanner(t, fr, testRestoreConfig())

	_, err := p.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetTempDB})
	if !errors.Is(err, ErrAmbiguousDump) {
		t.Errorf("err = %v, want ErrAmbiguousDump", err)
	}
}

func TestPlanner_PlanTempDB_PostgresDisabled(t *testing.T) {
	cfg := testRestoreConfig()
	cfg.Postgres.Enabled = false
	p := deterministicPlanner(t, &fakeRestic{}, cfg)

	_, err := p.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetTempDB})
	if !errors.Is(err, ErrDatabaseDisabled) {
		t.Errorf("err = %v, want ErrDatabaseDisabled", err)
	}
}

func TestPlanner_PlanTempDB_UnknownDatabase(t *testing.T) {
	fr := &fakeRestic{listFiles: []restic.FileInfo{
		{Path: "/var/backups/servervault/postgresql/a.dump.zst", Type: "file", Size: 10},
	}}
	p := deterministicPlanner(t, fr, testRestoreConfig())

	_, err := p.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: TargetTempDB, Database: "some_other_db"})
	var unknownDB *ErrUnknownDatabase
	if !errors.As(err, &unknownDB) {
		t.Errorf("err = %v, want *ErrUnknownDatabase", err)
	}
}

func TestPlanner_Plan_RequiresSnapshotID(t *testing.T) {
	p := deterministicPlanner(t, &fakeRestic{}, testRestoreConfig())
	if _, err := p.Plan(context.Background(), PlanOptions{Target: TargetFiles}); err == nil {
		t.Fatal("Plan with empty SnapshotID should fail")
	}
}

func TestPlanner_Plan_RejectsUnknownTarget(t *testing.T) {
	p := deterministicPlanner(t, &fakeRestic{}, testRestoreConfig())
	if _, err := p.Plan(context.Background(), PlanOptions{SnapshotID: "abc", Target: "bogus"}); err == nil {
		t.Fatal("Plan with an unknown target should fail")
	}
}

func TestNewPlanner_RequiresNonNilArgs(t *testing.T) {
	if _, err := NewPlanner(nil, testRestoreConfig()); err == nil {
		t.Error("NewPlanner with a nil restic client should fail")
	}
	if _, err := NewPlanner(&fakeRestic{}, nil); err == nil {
		t.Error("NewPlanner with a nil config should fail")
	}
}
