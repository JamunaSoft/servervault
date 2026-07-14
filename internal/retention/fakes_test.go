package retention

import (
	"context"
	"sync"

	"github.com/JamunaSoft/servervault/internal/restic"
)

// fakeRestic is a ResticClient test double.
type fakeRestic struct {
	mu sync.Mutex

	snapshots    []restic.Snapshot
	snapshotsErr error

	checkErr       error
	checkCallCount int

	// forgetSequence, if non-empty, provides a distinct ForgetSummary for
	// each successive Forget call (consumed in order, the last entry
	// repeating once exhausted) -- lets a test simulate the repository
	// changing between Plan's dry run and Execute's revalidation dry
	// run. If empty, every call returns forgetSummary.
	forgetSequence []restic.ForgetSummary
	forgetSummary  restic.ForgetSummary
	forgetErr      error
	forgetCalls    []restic.ForgetOptions
}

func (f *fakeRestic) Snapshots(_ context.Context, _ restic.SnapshotsOptions) ([]restic.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshots, f.snapshotsErr
}

func (f *fakeRestic) Check(_ context.Context, _ restic.CheckOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkCallCount++
	return f.checkErr
}

func (f *fakeRestic) Forget(_ context.Context, opts restic.ForgetOptions) (restic.ForgetSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forgetCalls = append(f.forgetCalls, opts)
	if f.forgetErr != nil {
		return restic.ForgetSummary{}, f.forgetErr
	}
	if len(f.forgetSequence) > 0 {
		idx := len(f.forgetCalls) - 1
		if idx >= len(f.forgetSequence) {
			idx = len(f.forgetSequence) - 1
		}
		return f.forgetSequence[idx], nil
	}
	return f.forgetSummary, nil
}

func (f *fakeRestic) forgetCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.forgetCalls)
}

func (f *fakeRestic) lastForgetCall() restic.ForgetOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.forgetCalls) == 0 {
		return restic.ForgetOptions{}
	}
	return f.forgetCalls[len(f.forgetCalls)-1]
}

func (f *fakeRestic) getCheckCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checkCallCount
}

// snapshotIDs builds n fake snapshots with sequential IDs, for tests that
// only care about the count.
func snapshotIDs(n int) []restic.Snapshot {
	out := make([]restic.Snapshot, n)
	for i := range out {
		out[i] = restic.Snapshot{ID: string(rune('a' + i))}
	}
	return out
}
