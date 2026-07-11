package doctor

import (
	"context"
	"testing"

	"github.com/JamunaSoft/servervault/internal/config"
)

func TestStatus_String(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusOK, "OK"},
		{StatusWarn, "WARN"},
		{StatusFail, "FAIL"},
		{StatusSkip, "SKIP"},
		{Status(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestReport_Failed(t *testing.T) {
	tests := []struct {
		name   string
		checks []Check
		want   bool
	}{
		{name: "all OK", checks: []Check{{Status: StatusOK}, {Status: StatusOK}}, want: false},
		{name: "warn only", checks: []Check{{Status: StatusOK}, {Status: StatusWarn}}, want: false},
		{name: "skip only", checks: []Check{{Status: StatusOK}, {Status: StatusSkip}}, want: false},
		{name: "one fail", checks: []Check{{Status: StatusOK}, {Status: StatusFail}}, want: true},
		{name: "empty", checks: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Report{Checks: tt.checks}
			if got := r.Failed(); got != tt.want {
				t.Errorf("Report.Failed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRun_UsesDefaultCollaboratorsWhenNil(t *testing.T) {
	cfg := baseConfig()

	// Options.Commands and Options.FreeBytes are left nil on purpose: Run
	// must fall back to real implementations rather than panicking on a
	// nil interface/func call. Restic/Postgres are injected fakes so this
	// test never invokes a real binary or touches the network -- their
	// own nil-fallback (constructing a real client from Config) is
	// covered by TestRun_ConstructsResticAndPostgresFromConfigWhenNil
	// below, using a config that safely Skips instead of dialing out.
	report := Run(context.Background(), Options{
		Config:   cfg,
		Restic:   fakeResticAccessChecker{},
		Postgres: fakePostgresPinger{},
	})

	if len(report.Checks) == 0 {
		t.Fatal("Run() returned a report with no checks")
	}

	names := map[string]bool{}
	for _, c := range report.Checks {
		names[c.Name] = true
	}
	for _, want := range []string{
		"OS/architecture", "required commands", "config validation", "secret permissions",
		"backup paths", "restore staging overlap (realpath)", "local disk space", "timezone",
		"Restic repository access", "PostgreSQL connectivity", "backup lock state",
	} {
		if !names[want] {
			t.Errorf("Run() report missing expected check %q", want)
		}
	}
}

func TestRun_ConstructsResticAndPostgresFromConfigWhenNil(t *testing.T) {
	// A config with no repository and Postgres disabled means Run's
	// nil-fallback construction (restic.New/postgres.New) never actually
	// needs to dial out -- both checks Skip immediately -- so this
	// exercises the fallback path itself without a real subprocess call.
	cfg := config.Defaults()
	cfg.Restic.Repository = ""
	cfg.Postgres.Enabled = false

	report := Run(context.Background(), Options{Config: cfg})

	for _, name := range []string{"Restic repository access", "PostgreSQL connectivity"} {
		found := false
		for _, c := range report.Checks {
			if c.Name == name {
				found = true
				if c.Status != StatusSkip {
					t.Errorf("%s status = %v, want StatusSkip", name, c.Status)
				}
			}
		}
		if !found {
			t.Errorf("Run() report missing check %q", name)
		}
	}
}

func TestRun_DeferredChecksAreSkippedNotFailed(t *testing.T) {
	report := Run(context.Background(), Options{
		Config:   baseConfig(),
		Restic:   fakeResticAccessChecker{},
		Postgres: fakePostgresPinger{},
	})

	skipCount := 0
	for _, c := range report.Checks {
		if c.Status == StatusSkip {
			skipCount++
		}
	}
	if skipCount < 2 {
		t.Errorf("Run() report has %d StatusSkip checks, want at least 2 (the still-deferred checks)", skipCount)
	}
	// A fresh Defaults()-based config (no real password file, no real
	// backup paths) is expected to fail some checks — but Skip statuses
	// must never be the reason Failed() is false when it shouldn't be, or
	// true when it shouldn't be: Failed() only reacts to StatusFail.
	if report.Failed() {
		hasFail := false
		for _, c := range report.Checks {
			if c.Status == StatusFail {
				hasFail = true
			}
		}
		if !hasFail {
			t.Error("Report.Failed() = true but no check has StatusFail")
		}
	}
}
