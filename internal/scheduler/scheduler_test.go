package scheduler

import (
	"testing"
	"time"
)

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("IANA timezone database entry %q not available in this environment: %v", name, err)
	}
	return loc
}

func TestSchedule_Validate(t *testing.T) {
	utc := time.UTC
	tests := []struct {
		name    string
		s       Schedule
		wantErr bool
	}{
		{"valid hourly", Schedule{Name: "x", Frequency: FrequencyHourly, Location: utc}, false},
		{"valid daily", Schedule{Name: "x", Frequency: FrequencyDaily, At: "02:00", Location: utc}, false},
		{"valid weekly", Schedule{Name: "x", Frequency: FrequencyWeekly, At: "02:00", Weekday: time.Sunday, Location: utc}, false},
		{"missing name", Schedule{Frequency: FrequencyHourly, Location: utc}, true},
		{"nil location", Schedule{Name: "x", Frequency: FrequencyHourly}, true},
		{"unknown frequency", Schedule{Name: "x", Frequency: "yearly", Location: utc}, true},
		{"daily missing at", Schedule{Name: "x", Frequency: FrequencyDaily, Location: utc}, true},
		{"daily bad at format", Schedule{Name: "x", Frequency: FrequencyDaily, At: "2am", Location: utc}, true},
		{"daily hour out of range", Schedule{Name: "x", Frequency: FrequencyDaily, At: "24:00", Location: utc}, true},
		{"daily minute out of range", Schedule{Name: "x", Frequency: FrequencyDaily, At: "10:60", Location: utc}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.s.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSchedule_NextRun_Hourly(t *testing.T) {
	s := Schedule{Name: "hourly", Frequency: FrequencyHourly, Location: time.UTC}

	after := time.Date(2026, 3, 15, 10, 30, 0, 0, time.UTC)
	got, err := s.NextRun(after)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want := time.Date(2026, 3, 15, 11, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextRun(%v) = %v, want %v", after, got, want)
	}

	// Exactly on the hour: next run is the *next* hour, not the same
	// instant -- NextRun is strictly after `after`.
	onHour := time.Date(2026, 3, 15, 11, 0, 0, 0, time.UTC)
	got2, err := s.NextRun(onHour)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want2 := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	if !got2.Equal(want2) {
		t.Errorf("NextRun(%v) = %v, want %v", onHour, got2, want2)
	}
}

func TestSchedule_NextRun_Daily(t *testing.T) {
	s := Schedule{Name: "daily", Frequency: FrequencyDaily, At: "02:00", Location: time.UTC}

	tests := []struct {
		after time.Time
		want  time.Time
	}{
		{
			after: time.Date(2026, 3, 15, 1, 0, 0, 0, time.UTC),
			want:  time.Date(2026, 3, 15, 2, 0, 0, 0, time.UTC),
		},
		{
			// After today's run time -- rolls to tomorrow.
			after: time.Date(2026, 3, 15, 3, 0, 0, 0, time.UTC),
			want:  time.Date(2026, 3, 16, 2, 0, 0, 0, time.UTC),
		},
		{
			// Exactly at today's run time -- still rolls to tomorrow
			// (strictly after).
			after: time.Date(2026, 3, 15, 2, 0, 0, 0, time.UTC),
			want:  time.Date(2026, 3, 16, 2, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		got, err := s.NextRun(tt.after)
		if err != nil {
			t.Fatalf("NextRun(%v): %v", tt.after, err)
		}
		if !got.Equal(tt.want) {
			t.Errorf("NextRun(%v) = %v, want %v", tt.after, got, tt.want)
		}
	}
}

func TestSchedule_NextRun_Weekly(t *testing.T) {
	s := Schedule{Name: "weekly", Frequency: FrequencyWeekly, At: "03:00", Weekday: time.Sunday, Location: time.UTC}

	// 2026-03-15 is a Sunday in this timezone-free UTC test.
	sunday := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	if sunday.Weekday() != time.Sunday {
		t.Fatalf("test fixture error: %v is not a Sunday", sunday)
	}

	// Midweek -> next Sunday.
	wednesday := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)
	got, err := s.NextRun(wednesday)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want := time.Date(2026, 3, 15, 3, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextRun(%v) = %v, want %v", wednesday, got, want)
	}

	// Sunday morning before the run time -> today.
	sundayEarly := time.Date(2026, 3, 15, 1, 0, 0, 0, time.UTC)
	got2, err := s.NextRun(sundayEarly)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	if !got2.Equal(want) {
		t.Errorf("NextRun(%v) = %v, want %v", sundayEarly, got2, want)
	}

	// Sunday after the run time -> next Sunday, 7 days later.
	sundayLate := time.Date(2026, 3, 15, 4, 0, 0, 0, time.UTC)
	got3, err := s.NextRun(sundayLate)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want3 := time.Date(2026, 3, 22, 3, 0, 0, 0, time.UTC)
	if !got3.Equal(want3) {
		t.Errorf("NextRun(%v) = %v, want %v", sundayLate, got3, want3)
	}
}

// TestSchedule_NextRun_DST_SpringForward covers America/New_York's 2026
// spring-forward transition (clocks jump from 01:59:59 to 03:00:00 on
// 2026-03-08). A daily schedule set to run at 02:30 -- a wall-clock time
// that does not exist that day -- must still return *a* well-defined
// instant, using Go's documented normalization rule, not panic or return
// a wrong-by-an-hour result silently.
func TestSchedule_NextRun_DST_SpringForward(t *testing.T) {
	loc := mustLoadLocation(t, "America/New_York")
	s := Schedule{Name: "daily", Frequency: FrequencyDaily, At: "02:30", Location: loc}

	before := time.Date(2026, 3, 8, 0, 0, 0, 0, loc) // 00:00 EST, before the gap
	got, err := s.NextRun(before)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}

	// Go's time.Date normalizes a nonexistent wall-clock time by adding
	// the "missing" offset -- 02:30 local, which doesn't exist, becomes
	// 03:30 EDT. We assert the documented, deterministic behavior
	// (matches what time.Date(2026,3,8,2,30,0,0,loc) itself normalizes
	// to) rather than hardcoding a wall-clock assumption independent of
	// what Go's stdlib actually does.
	want := time.Date(2026, 3, 8, 2, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextRun across spring-forward = %v, want %v (Go's normalized form of the nonexistent wall time)", got, want)
	}
	// Whatever it normalizes to, it must be a real, unambiguous instant
	// strictly after `before`, and UTC round-trip must be stable.
	if !got.After(before) {
		t.Errorf("NextRun across spring-forward = %v, not after %v", got, before)
	}
	if rt := got.UTC(); rt.IsZero() {
		t.Errorf("NextRun across spring-forward produced a zero-value instant")
	}
}

// TestSchedule_NextRun_DST_FallBack covers the 2026-11-01 fall-back
// transition (clocks repeat 01:00-01:59 local). A schedule at 01:30 is
// ambiguous that day; NextRun must still return a single, well-defined
// instant (Go's rule: the first occurrence, pre-transition offset).
func TestSchedule_NextRun_DST_FallBack(t *testing.T) {
	loc := mustLoadLocation(t, "America/New_York")
	s := Schedule{Name: "daily", Frequency: FrequencyDaily, At: "01:30", Location: loc}

	before := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	got, err := s.NextRun(before)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want := time.Date(2026, 11, 1, 1, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextRun across fall-back = %v, want %v", got, want)
	}
}

func TestSchedule_NextRun_TimezoneChangesResultVsUTC(t *testing.T) {
	ny := mustLoadLocation(t, "America/New_York")
	s := Schedule{Name: "daily", Frequency: FrequencyDaily, At: "02:00", Location: ny}

	after := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) // interpreted in ny via .In() inside NextRun
	got, err := s.NextRun(after)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	if got.Location().String() != ny.String() {
		t.Errorf("NextRun location = %v, want %v", got.Location(), ny)
	}
	// 2026-06-01 is in EDT (UTC-4); 02:00 local = 06:00 UTC.
	if got.UTC().Hour() != 6 {
		t.Errorf("NextRun in UTC hour = %d, want 6 (02:00 EDT)", got.UTC().Hour())
	}
}

func TestSchedule_CatchUp(t *testing.T) {
	s := Schedule{Name: "daily", Frequency: FrequencyDaily, At: "02:00", Location: time.UTC}

	lastRun := time.Date(2026, 3, 10, 2, 0, 0, 0, time.UTC)
	now := time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC) // 3 missed runs: 11th, 12th, 13th

	t.Run("skip policy reports missed count but does not run now", func(t *testing.T) {
		result, err := s.CatchUp(lastRun, now, MissedRunSkip)
		if err != nil {
			t.Fatalf("CatchUp: %v", err)
		}
		if result.Missed != 3 {
			t.Errorf("Missed = %d, want 3", result.Missed)
		}
		if result.RunNow {
			t.Error("RunNow = true, want false for MissedRunSkip")
		}
		wantNext := time.Date(2026, 3, 14, 2, 0, 0, 0, time.UTC)
		if !result.NextRun.Equal(wantNext) {
			t.Errorf("NextRun = %v, want %v", result.NextRun, wantNext)
		}
	})

	t.Run("run_once policy requests an immediate run", func(t *testing.T) {
		result, err := s.CatchUp(lastRun, now, MissedRunRunOnce)
		if err != nil {
			t.Fatalf("CatchUp: %v", err)
		}
		if result.Missed != 3 {
			t.Errorf("Missed = %d, want 3", result.Missed)
		}
		if !result.RunNow {
			t.Error("RunNow = false, want true for MissedRunRunOnce with missed occurrences")
		}
	})

	t.Run("no missed occurrences never runs now even under run_once", func(t *testing.T) {
		recentLastRun := time.Date(2026, 3, 13, 2, 0, 0, 0, time.UTC)
		result, err := s.CatchUp(recentLastRun, now, MissedRunRunOnce)
		if err != nil {
			t.Fatalf("CatchUp: %v", err)
		}
		if result.Missed != 0 {
			t.Errorf("Missed = %d, want 0", result.Missed)
		}
		if result.RunNow {
			t.Error("RunNow = true, want false when nothing was missed")
		}
	})
}

func TestConcurrencyLimit_Allow(t *testing.T) {
	tests := []struct {
		name    string
		limit   ConcurrencyLimit
		current int
		want    bool
	}{
		{"zero means unlimited", ConcurrencyLimit{Max: 0}, 1000, true},
		{"under limit", ConcurrencyLimit{Max: 3}, 2, true},
		{"at limit", ConcurrencyLimit{Max: 3}, 3, false},
		{"over limit", ConcurrencyLimit{Max: 3}, 4, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.limit.Allow(tt.current); got != tt.want {
				t.Errorf("Allow(%d) = %v, want %v", tt.current, got, tt.want)
			}
		})
	}
}
