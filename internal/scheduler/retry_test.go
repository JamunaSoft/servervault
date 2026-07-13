package scheduler

import (
	"math/rand"
	"testing"
	"time"
)

func TestRetryPolicy_Delay_ExponentialAndDeterministicWithoutJitter(t *testing.T) {
	p := RetryPolicy{
		MaxAttempts: 10,
		BaseDelay:   time.Second,
		MaxDelay:    time.Minute,
		Jitter:      NoJitter,
	}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 32 * time.Second},
		{7, 60 * time.Second}, // 64s capped to MaxDelay
		{20, 60 * time.Second},
	}
	for _, tt := range tests {
		if got := p.Delay(tt.attempt); got != tt.want {
			t.Errorf("Delay(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestRetryPolicy_Delay_NeverExceedsMaxDelay(t *testing.T) {
	p := RetryPolicy{BaseDelay: time.Second, MaxDelay: 30 * time.Second, Jitter: NoJitter}
	for attempt := 1; attempt <= 50; attempt++ {
		d := p.Delay(attempt)
		if d > p.MaxDelay {
			t.Errorf("Delay(%d) = %v, exceeds MaxDelay %v", attempt, d, p.MaxDelay)
		}
		if d < 0 {
			t.Errorf("Delay(%d) = %v, negative", attempt, d)
		}
	}
}

func TestRetryPolicy_Delay_AttemptBelowOneClampedToOne(t *testing.T) {
	p := RetryPolicy{BaseDelay: time.Second, MaxDelay: time.Minute, Jitter: NoJitter}
	got0 := p.Delay(0)
	got1 := p.Delay(1)
	if got0 != got1 {
		t.Errorf("Delay(0) = %v, want same as Delay(1) = %v", got0, got1)
	}
}

func TestRetryPolicy_Delay_DeterministicJitterIsReproducible(t *testing.T) {
	// Two RetryPolicy instances built with independently-seeded, same-seed
	// random sources must produce identical sequences -- this is the
	// "jitter must be injectable/deterministic in tests" requirement.
	p1 := RetryPolicy{BaseDelay: time.Second, MaxDelay: time.Minute, Jitter: NewRandomJitter(rand.New(rand.NewSource(42)))}
	p2 := RetryPolicy{BaseDelay: time.Second, MaxDelay: time.Minute, Jitter: NewRandomJitter(rand.New(rand.NewSource(42)))}

	for attempt := 1; attempt <= 10; attempt++ {
		d1 := p1.Delay(attempt)
		d2 := p2.Delay(attempt)
		if d1 != d2 {
			t.Errorf("attempt %d: same-seed jitter diverged: %v vs %v", attempt, d1, d2)
		}
	}
}

func TestRetryPolicy_Delay_FullJitterStaysInBounds(t *testing.T) {
	p := RetryPolicy{BaseDelay: time.Second, MaxDelay: 10 * time.Second, Jitter: NewRandomJitter(rand.New(rand.NewSource(7)))}
	for attempt := 1; attempt <= 20; attempt++ {
		d := p.Delay(attempt)
		if d < 0 || d > p.MaxDelay {
			t.Errorf("Delay(%d) = %v, out of bounds [0, %v]", attempt, d, p.MaxDelay)
		}
	}
}

func TestRetryPolicy_ShouldRetry(t *testing.T) {
	unlimited := RetryPolicy{MaxAttempts: 0}
	if !unlimited.ShouldRetry(1000) {
		t.Error("MaxAttempts=0 should mean unlimited retries")
	}

	limited := RetryPolicy{MaxAttempts: 3}
	if !limited.ShouldRetry(3) {
		t.Error("ShouldRetry(3) with MaxAttempts=3 should be true")
	}
	if limited.ShouldRetry(4) {
		t.Error("ShouldRetry(4) with MaxAttempts=3 should be false")
	}
}

func TestNoJitter_ReturnsBaseUnchanged(t *testing.T) {
	if got := NoJitter(5, 3*time.Second); got != 3*time.Second {
		t.Errorf("NoJitter = %v, want unchanged 3s", got)
	}
}
