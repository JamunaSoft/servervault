package restore

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/JamunaSoft/servervault/internal/restic"
)

// fakeRestic is a ResticClient test double. writeFileOnRestore, when
// true, simulates a real restic restore actually landing content on disk
// -- both Executor.verify (TargetFiles) and executeTempDB's post-restore
// os.Stat check depend on something really being there, so a fake that
// only recorded the call without writing anything would make every
// executor test either lie about coverage or need its own ad hoc
// workaround.
type fakeRestic struct {
	mu sync.Mutex

	stats    restic.Stats
	statsErr error

	listFiles []restic.FileInfo
	listErr   error

	restoreSummary     restic.RestoreSummary
	restoreErr         error
	writeFileOnRestore bool

	restoreCalls []restic.RestoreOptions
}

func (f *fakeRestic) Stats(context.Context, string) (restic.Stats, error) {
	return f.stats, f.statsErr
}

func (f *fakeRestic) List(context.Context, string, string) ([]restic.FileInfo, error) {
	return f.listFiles, f.listErr
}

func (f *fakeRestic) Restore(_ context.Context, opts restic.RestoreOptions) (restic.RestoreSummary, error) {
	f.mu.Lock()
	f.restoreCalls = append(f.restoreCalls, opts)
	f.mu.Unlock()

	if f.restoreErr != nil {
		return restic.RestoreSummary{}, f.restoreErr
	}
	if f.writeFileOnRestore {
		var target string
		if opts.Include != "" {
			target = filepath.Join(opts.Target, opts.Include)
			_ = os.MkdirAll(filepath.Dir(target), 0o700)
		} else {
			_ = os.MkdirAll(opts.Target, 0o700)
			target = filepath.Join(opts.Target, "restored-file.txt")
		}
		_ = os.WriteFile(target, []byte("fake restored content"), 0o600)
	}
	return f.restoreSummary, nil
}

func (f *fakeRestic) restoreCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.restoreCalls)
}

// fakePostgres is a PostgresClient test double, backed by an in-memory
// set of "existing" database names so DatabaseExists/CreateDatabase/
// DropDatabase behave consistently with each other across a test.
type fakePostgres struct {
	mu sync.Mutex

	existing map[string]bool

	existsErr  error
	createErr  error
	dropErr    error
	restoreErr error
	pingErr    error

	createCalls  []string
	dropCalls    []string
	restoreCalls []restoreToTempCall
}

type restoreToTempCall struct {
	DumpPath string
	Database string
}

func newFakePostgres() *fakePostgres {
	return &fakePostgres{existing: map[string]bool{}}
}

func (f *fakePostgres) DatabaseExists(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return f.existing[name], nil
}

func (f *fakePostgres) CreateDatabase(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls = append(f.createCalls, name)
	if f.createErr != nil {
		return f.createErr
	}
	f.existing[name] = true
	return nil
}

func (f *fakePostgres) DropDatabase(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dropCalls = append(f.dropCalls, name)
	if f.dropErr != nil {
		return f.dropErr
	}
	delete(f.existing, name)
	return nil
}

func (f *fakePostgres) RestoreToTemp(_ context.Context, dumpPath, database string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restoreCalls = append(f.restoreCalls, restoreToTempCall{DumpPath: dumpPath, Database: database})
	return f.restoreErr
}

func (f *fakePostgres) PingDatabase(context.Context, string) error {
	return f.pingErr
}

func (f *fakePostgres) createCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.createCalls)
}

func (f *fakePostgres) dropCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.dropCalls)
}
