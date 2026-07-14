package retention

import (
	"context"
	"errors"
	"testing"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/restic"
)

func testRetentionConfig() *config.Config {
	cfg := config.Defaults()
	cfg.HostTag = "test-host"
	cfg.Restic.Repository = "local:/tmp/does-not-matter"
	cfg.Retention.KeepDaily = 7
	cfg.Retention.KeepWeekly = 4
	cfg.Retention.KeepMonthly = 12
	cfg.Retention.MinKeepTotal = 1
	cfg.Retention.MaxDeleteCount = 50
	cfg.Retention.LockFile = "/run/lock/servervault-prune.lock"
	return cfg
}

func TestPlanner_Plan_Success(t *testing.T) {
	fr := &fakeRestic{
		snapshots: snapshotIDs(5),
		forgetSummary: restic.ForgetSummary{
			KeptSnapshotIDs:    []string{"k1", "k2", "k3"},
			RemovedSnapshotIDs: []string{"r1", "r2"},
		},
	}
	cfg := testRetentionConfig()
	p, err := NewPlanner(fr, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	plan, err := p.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.CurrentSnapshotCount != 5 {
		t.Errorf("CurrentSnapshotCount = %d, want 5", plan.CurrentSnapshotCount)
	}
	if plan.RemoveCount != 2 {
		t.Errorf("RemoveCount = %d, want 2", plan.RemoveCount)
	}
	if plan.RemainingAfterPrune != 3 {
		t.Errorf("RemainingAfterPrune = %d, want 3", plan.RemainingAfterPrune)
	}
	if len(plan.SafetyChecks) == 0 {
		t.Error("SafetyChecks should be populated")
	}
	if fr.getCheckCallCount() != 1 {
		t.Errorf("Check called %d times, want 1", fr.getCheckCallCount())
	}

	// Plan's Forget call must be a dry run, scoped to this host and the
	// servervault tag.
	call := fr.lastForgetCall()
	if !call.DryRun {
		t.Error("Plan's Forget call must set DryRun")
	}
	if call.Prune {
		t.Error("Plan's Forget call must never set Prune -- Plan performs no writes")
	}
	if call.Host != "test-host" {
		t.Errorf("Host = %q, want test-host", call.Host)
	}
	if len(call.Tags) == 0 || call.Tags[0] != "servervault" {
		t.Errorf("Tags = %v, want to start with servervault", call.Tags)
	}
}

func TestPlanner_Plan_RepositoryUnhealthy(t *testing.T) {
	fr := &fakeRestic{
		snapshots: snapshotIDs(5),
		checkErr:  errors.New("repository check failed: mismatched index"),
	}
	p, err := NewPlanner(fr, testRetentionConfig())
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	_, err = p.Plan(context.Background())
	var unhealthy *ErrRepositoryUnhealthy
	if !errors.As(err, &unhealthy) {
		t.Fatalf("err = %v, want *ErrRepositoryUnhealthy", err)
	}
	if fr.forgetCallCount() != 0 {
		t.Error("Forget must never be called when the health check fails")
	}
}

func TestPlanner_Plan_BelowMinimumSnapshots(t *testing.T) {
	fr := &fakeRestic{
		snapshots: snapshotIDs(2),
		forgetSummary: restic.ForgetSummary{
			RemovedSnapshotIDs: []string{"r1"}, // leaves 1
		},
	}
	cfg := testRetentionConfig()
	cfg.Retention.MinKeepTotal = 2 // would leave 1, below the floor
	p, err := NewPlanner(fr, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	_, err = p.Plan(context.Background())
	if !errors.Is(err, ErrBelowMinimumSnapshots) {
		t.Errorf("err = %v, want ErrBelowMinimumSnapshots", err)
	}
}

func TestPlanner_Plan_MaxDeleteExceeded(t *testing.T) {
	fr := &fakeRestic{
		snapshots: snapshotIDs(10),
		forgetSummary: restic.ForgetSummary{
			RemovedSnapshotIDs: []string{"r1", "r2", "r3"},
		},
	}
	cfg := testRetentionConfig()
	cfg.Retention.MaxDeleteCount = 1
	p, err := NewPlanner(fr, cfg)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	_, err = p.Plan(context.Background())
	var maxErr *ErrMaxDeleteExceeded
	if !errors.As(err, &maxErr) {
		t.Fatalf("err = %v, want *ErrMaxDeleteExceeded", err)
	}
	if maxErr.PlannedCount != 3 || maxErr.MaxAllowed != 1 {
		t.Errorf("maxErr = %+v, want PlannedCount=3 MaxAllowed=1", maxErr)
	}
}

func TestPlanner_Plan_ListSnapshotsFailure(t *testing.T) {
	fr := &fakeRestic{snapshotsErr: errors.New("connection refused")}
	p, err := NewPlanner(fr, testRetentionConfig())
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	if _, err := p.Plan(context.Background()); err == nil {
		t.Fatal("Plan should fail when listing snapshots fails")
	}
	if fr.getCheckCallCount() != 0 {
		t.Error("Check must never be called when listing snapshots fails")
	}
	if fr.forgetCallCount() != 0 {
		t.Error("Forget must never be called when listing snapshots fails")
	}
}

func TestPlanner_Plan_DryRunForgetFailure(t *testing.T) {
	fr := &fakeRestic{
		snapshots: snapshotIDs(5),
		forgetErr: errors.New("repository locked"),
	}
	p, err := NewPlanner(fr, testRetentionConfig())
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	if _, err := p.Plan(context.Background()); err == nil {
		t.Fatal("Plan should fail when the dry-run forget fails")
	}
}

func TestNewPlanner_RequiresNonNilArgs(t *testing.T) {
	if _, err := NewPlanner(nil, testRetentionConfig()); err == nil {
		t.Error("NewPlanner with a nil restic client should fail")
	}
	if _, err := NewPlanner(&fakeRestic{}, nil); err == nil {
		t.Error("NewPlanner with a nil config should fail")
	}
}
