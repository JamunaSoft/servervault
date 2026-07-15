package health

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JamunaSoft/servervault/internal/config"
	"github.com/JamunaSoft/servervault/internal/job"
	"github.com/JamunaSoft/servervault/internal/lock"
)

type fakeRestic struct {
	err error
}

func (f *fakeRestic) CatConfig(context.Context) error { return f.err }

type fakeJobs struct {
	jobs map[job.Type]job.Job
	err  error
}

func (f *fakeJobs) LatestByType(_ context.Context, t job.Type) (job.Job, error) {
	if f.err != nil {
		return job.Job{}, f.err
	}
	j, ok := f.jobs[t]
	if !ok {
		return job.Job{}, job.ErrNotFound
	}
	return j, nil
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.Restic.Repository = "local:/tmp/does-not-matter"
	cfg.Backup.LockFile = dir + "/backup.lock"
	cfg.Restore.LockFile = dir + "/restore.lock"
	cfg.Retention.LockFile = dir + "/prune.lock"
	return cfg
}

func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestRun_AllHealthy(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	cfg := testConfig(t)
	opts := Options{
		Config: cfg,
		Restic: &fakeRestic{},
		Jobs: &fakeJobs{jobs: map[job.Type]job.Job{
			job.TypeBackup:  {Type: job.TypeBackup, State: job.StateCompleted, FinishedAt: now.Add(-1 * time.Hour)},
			job.TypeRestore: {Type: job.TypeRestore, State: job.StateCompleted, FinishedAt: now.Add(-72 * time.Hour)},
			job.TypePrune:   {Type: job.TypePrune, State: job.StateCompleted, FinishedAt: now.Add(-1 * time.Hour)},
		}},
		Now: fixedNow(now),
	}

	report := Run(context.Background(), opts)
	if report.Failed() {
		t.Errorf("report should not be failed: %+v", report.Checks)
	}
	for _, c := range report.Checks {
		if c.Status == StatusFail {
			t.Errorf("check %q unexpectedly failed: %s", c.Name, c.Detail)
		}
	}
	if report.GeneratedAt != now {
		t.Errorf("GeneratedAt = %v, want %v", report.GeneratedAt, now)
	}
}

func TestRun_NilOptions_ReportsUnknownNotPanicOrFail(t *testing.T) {
	report := Run(context.Background(), Options{})
	if report.Failed() {
		t.Errorf("an entirely unconfigured Options should never FAIL, only report UNKNOWN: %+v", report.Checks)
	}
	for _, c := range report.Checks {
		if c.Status != StatusUnknown {
			t.Errorf("check %q = %s, want UNKNOWN with nothing configured", c.Name, c.Status)
		}
	}
}

func TestCheckResticAccess(t *testing.T) {
	tests := []struct {
		name       string
		repository string
		restic     ResticAccessChecker
		want       Status
	}{
		{name: "not configured", repository: "", restic: &fakeRestic{}, want: StatusUnknown},
		{name: "no client", repository: "local:/tmp/x", restic: nil, want: StatusUnknown},
		{name: "reachable", repository: "local:/tmp/x", restic: &fakeRestic{}, want: StatusOK},
		{name: "unreachable", repository: "local:/tmp/x", restic: &fakeRestic{err: errors.New("connection refused")}, want: StatusFail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Restic.Repository = tt.repository
			got := checkResticAccess(context.Background(), Options{Config: cfg, Restic: tt.restic})
			if got.Status != tt.want {
				t.Errorf("Status = %s, want %s (detail: %s)", got.Status, tt.want, got.Detail)
			}
		})
	}
}

func TestCheckLockState(t *testing.T) {
	t.Run("not configured", func(t *testing.T) {
		got := checkLockState("backup", "")
		if got.Status != StatusUnknown {
			t.Errorf("Status = %s, want UNKNOWN", got.Status)
		}
	})

	t.Run("not held", func(t *testing.T) {
		path := t.TempDir() + "/x.lock"
		got := checkLockState("backup", path)
		if got.Status != StatusOK {
			t.Errorf("Status = %s, want OK (detail: %s)", got.Status, got.Detail)
		}
	})

	t.Run("held", func(t *testing.T) {
		path := t.TempDir() + "/x.lock"
		held, ok, err := lock.TryAcquire(path)
		if err != nil || !ok {
			t.Fatalf("setup: acquire lock: ok=%v err=%v", ok, err)
		}
		defer held.Release()

		got := checkLockState("backup", path)
		if got.Status != StatusWarn {
			t.Errorf("Status = %s, want WARN (detail: %s)", got.Status, got.Detail)
		}
	})
}

func TestCheckLastJob(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		jobs           JobLister
		checkFreshness bool
		want           Status
	}{
		{
			name: "no job store configured",
			jobs: nil,
			want: StatusUnknown,
		},
		{
			name: "never run",
			jobs: &fakeJobs{jobs: map[job.Type]job.Job{}},
			want: StatusUnknown,
		},
		{
			name: "recently completed",
			jobs: &fakeJobs{jobs: map[job.Type]job.Job{
				job.TypeBackup: {Type: job.TypeBackup, State: job.StateCompleted, FinishedAt: now.Add(-1 * time.Hour)},
			}},
			checkFreshness: true,
			want:           StatusOK,
		},
		{
			name: "completed but stale, freshness checked",
			jobs: &fakeJobs{jobs: map[job.Type]job.Job{
				job.TypeBackup: {Type: job.TypeBackup, State: job.StateCompleted, FinishedAt: now.Add(-72 * time.Hour)},
			}},
			checkFreshness: true,
			want:           StatusWarn,
		},
		{
			name: "completed but stale, freshness not checked",
			jobs: &fakeJobs{jobs: map[job.Type]job.Job{
				job.TypeBackup: {Type: job.TypeBackup, State: job.StateCompleted, FinishedAt: now.Add(-720 * time.Hour)},
			}},
			checkFreshness: false,
			want:           StatusOK,
		},
		{
			name: "failed",
			jobs: &fakeJobs{jobs: map[job.Type]job.Job{
				job.TypeBackup: {Type: job.TypeBackup, State: job.StateFailed, FinishedAt: now.Add(-1 * time.Hour), ErrorSummary: "boom"},
			}},
			want: StatusFail,
		},
		{
			name: "cancelled",
			jobs: &fakeJobs{jobs: map[job.Type]job.Job{
				job.TypeBackup: {Type: job.TypeBackup, State: job.StateCancelled, FinishedAt: now.Add(-1 * time.Hour)},
			}},
			want: StatusWarn,
		},
		{
			name: "interrupted",
			jobs: &fakeJobs{jobs: map[job.Type]job.Job{
				job.TypeBackup: {Type: job.TypeBackup, State: job.StateInterrupted, FinishedAt: now.Add(-1 * time.Hour)},
			}},
			want: StatusWarn,
		},
		{
			name: "still in progress",
			jobs: &fakeJobs{jobs: map[job.Type]job.Job{
				job.TypeBackup: {Type: job.TypeBackup, State: job.StateBackingUp},
			}},
			want: StatusOK,
		},
		{
			name: "store error",
			jobs: &fakeJobs{err: errors.New("database is locked")},
			want: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{Jobs: tt.jobs, Now: fixedNow(now), StaleAfter: 48 * time.Hour}
			got := checkLastJob(context.Background(), opts, job.TypeBackup, "last backup", tt.checkFreshness)
			if got.Status != tt.want {
				t.Errorf("Status = %s, want %s (detail: %s)", got.Status, tt.want, got.Detail)
			}
		})
	}
}

func TestReport_Failed(t *testing.T) {
	tests := []struct {
		name   string
		checks []Check
		want   bool
	}{
		{name: "all ok", checks: []Check{{Status: StatusOK}, {Status: StatusOK}}, want: false},
		{name: "warn only", checks: []Check{{Status: StatusOK}, {Status: StatusWarn}}, want: false},
		{name: "unknown only", checks: []Check{{Status: StatusUnknown}}, want: false},
		{name: "one fail", checks: []Check{{Status: StatusOK}, {Status: StatusFail}}, want: true},
		{name: "empty", checks: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Report{Checks: tt.checks}
			if got := r.Failed(); got != tt.want {
				t.Errorf("Failed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatus_String_And_MarshalJSON(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusOK, "OK"},
		{StatusWarn, "WARN"},
		{StatusFail, "FAIL"},
		{StatusUnknown, "UNKNOWN"},
		{Status(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
		b, err := tt.status.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON: %v", err)
		}
		if string(b) != `"`+tt.want+`"` {
			t.Errorf("MarshalJSON() = %s, want %q", b, tt.want)
		}
	}
}
