package backup

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/JamunaSoft/servervault/internal/lock"
	"github.com/JamunaSoft/servervault/internal/restic"
)

// blockingBacker holds restic.Backup open until proceed is closed, so the
// concurrency test below can guarantee every other goroutine has already
// attempted (and failed) to acquire the lock before the winner is allowed
// to finish -- making the test deterministic rather than dependent on
// goroutine scheduling luck.
type blockingBacker struct {
	proceed chan struct{}
	summary restic.Summary
}

func (b *blockingBacker) Backup(ctx context.Context, opts restic.BackupOptions) (restic.Summary, error) {
	<-b.proceed
	return b.summary, nil
}

// TestEngine_Run_ConcurrentCallsOnlyOneSucceeds exercises real flock
// concurrency (internal/lock), not a fake: N goroutines race to acquire
// the same lock file at (as close as Go can arrange) the same instant.
// Exactly one must win; every other must get lock.ErrLocked immediately,
// never after waiting. It uses fake Dumper/Backer -- concurrency
// correctness is a property of internal/lock's flock usage and
// Engine.Run's own logic, not of restic/postgres's real behavior, so a
// real backend would only make this test slower and flakier without
// proving anything more.
func TestEngine_Run_ConcurrentCallsOnlyOneSucceeds(t *testing.T) {
	cfg := testConfig(t)
	cfg.Postgres.Enabled = false

	const n = 5
	proceed := make(chan struct{})
	backer := &blockingBacker{proceed: proceed, summary: restic.Summary{SnapshotID: "concurrent-test"}}

	start := make(chan struct{})
	attempted := make(chan struct{}, n)
	errs := make([]error, n)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := testEngine(t, cfg, nil, backer)
			<-start
			_, err := e.Run(context.Background())
			errs[i] = err
			attempted <- struct{}{}
		}(i)
	}

	close(start)

	// Collect the n-1 fast (busy) completions. The winner is blocked
	// inside blockingBacker.Backup and cannot have signaled yet, so this
	// loop can only make progress via the losers -- guaranteeing every
	// busy attempt happened while the lock was still held, with no race
	// against the winner's own completion.
	for i := 0; i < n-1; i++ {
		<-attempted
	}
	close(proceed)
	<-attempted // the winner's own completion
	wg.Wait()

	successes, busy := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, lock.ErrLocked):
			busy++
		default:
			t.Errorf("unexpected error from a concurrent Run(): %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("successes = %d, want exactly 1 (%d busy, %d other)", successes, busy, n-successes-busy)
	}
	if busy != n-1 {
		t.Errorf("busy (lock.ErrLocked) = %d, want %d", busy, n-1)
	}

	// The lock must be free again afterward.
	held, _, err := lock.Status(cfg.Backup.LockFile)
	if err != nil {
		t.Fatalf("lock.Status(): unexpected error: %v", err)
	}
	if held {
		t.Error("lock still held after all concurrent Run() calls returned")
	}
}
