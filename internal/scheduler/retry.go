package scheduler

import (
	"math"
	"math/rand"
	"time"
)

// JitterFunc perturbs a computed base delay for attempt (1-indexed).
// Implementations must return a duration in [0, base] for the "full
// jitter" style used by NewRandomJitter, but the interface itself makes
// no such promise -- NoJitter, for instance, returns base unchanged.
type JitterFunc func(attempt int, base time.Duration) time.Duration

// NoJitter returns base unchanged. Used by default and in tests that need
// RetryPolicy.Delay to be exactly reproducible.
func NoJitter(_ int, base time.Duration) time.Duration { return base }

// NewRandomJitter returns a JitterFunc implementing "full jitter"
// (https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/):
// a uniformly random duration in [0, base]. It is seeded from r, which
// tests should construct with a fixed seed (rand.New(rand.NewSource(seed)))
// to keep results deterministic -- production callers can use a
// time-seeded source.
func NewRandomJitter(r *rand.Rand) JitterFunc {
	return func(_ int, base time.Duration) time.Duration {
		if base <= 0 {
			return 0
		}
		return time.Duration(r.Int63n(int64(base) + 1))
	}
}

// RetryPolicy configures bounded exponential backoff between retry
// attempts.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	// Jitter is applied to the computed exponential delay before it's
	// returned. Nil means NoJitter (deterministic).
	Jitter JitterFunc
}

// Delay returns how long to wait before retry attempt `attempt` (1 =
// first retry, after the initial try). It is always in
// [0, MaxDelay] and grows exponentially with attempt until MaxDelay caps
// it: BaseDelay * 2^(attempt-1), capped, then passed through Jitter.
//
// Delay does not consult MaxAttempts -- callers stop retrying once
// attempt > MaxAttempts themselves; ShouldRetry does that check.
func (p RetryPolicy) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	jitter := p.Jitter
	if jitter == nil {
		jitter = NoJitter
	}

	// 2^(attempt-1), guarded against overflow for large attempt counts by
	// capping the exponent itself before multiplying.
	const maxShift = 32
	shift := attempt - 1
	if shift > maxShift {
		shift = maxShift
	}
	multiplier := math.Pow(2, float64(shift))

	base := time.Duration(float64(p.BaseDelay) * multiplier)
	if base <= 0 || base > p.MaxDelay {
		base = p.MaxDelay
	}
	if base < 0 {
		base = 0
	}

	delayed := jitter(attempt, base)
	if delayed < 0 {
		delayed = 0
	}
	if delayed > p.MaxDelay {
		delayed = p.MaxDelay
	}
	return delayed
}

// ShouldRetry reports whether attempt (1 = first retry) is still within
// MaxAttempts. MaxAttempts <= 0 means unlimited retries.
func (p RetryPolicy) ShouldRetry(attempt int) bool {
	if p.MaxAttempts <= 0 {
		return true
	}
	return attempt <= p.MaxAttempts
}
