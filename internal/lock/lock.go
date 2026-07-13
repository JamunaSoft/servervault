// Package lock provides advisory file locking to prevent concurrent
// ServerVault operations (e.g. two overlapping backups) on the same host.
//
// It is built on flock(2), not a PID file: flock is kernel-managed and
// atomic, and the kernel releases it automatically when the holding
// process's file descriptor is closed — including on a crash or SIGKILL.
// There is no "stale lock" problem to clean up. The lock file itself is
// never deleted, only ever created-if-missing and flocked; deleting and
// recreating it would reopen a classic flock race (a new file at the same
// path gets a new inode, whose lock doesn't conflict with an orphaned
// holder's lock on the old, unlinked inode).
package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrLocked is returned by Acquire when another process already holds the
// lock.
var ErrLocked = errors.New("lock: already held by another process")

// Lock is a held advisory lock. Release must be called exactly once;
// calling it again is a safe no-op.
type Lock struct {
	file     *os.File
	released bool
}

// Acquire attempts to acquire an exclusive, non-blocking lock at path,
// creating the file (and its parent directory) if necessary. It returns
// ErrLocked immediately if another process already holds the lock —
// Acquire never blocks waiting for a lock, matching the shell
// implementation's `flock -n` semantics: a second concurrent operation
// fails fast rather than queuing.
func Acquire(path string) (*Lock, error) {
	l, ok, err := TryAcquire(path)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLocked
	}
	return l, nil
}

// TryAcquire is like Acquire, but reports the busy case via ok=false
// instead of an error, so callers (such as doctor's lock-state check) can
// distinguish "lock busy" from "couldn't even open/create the lock file"
// without matching error strings.
func TryAcquire(path string) (l *Lock, ok bool, err error) {
	if path == "" {
		return nil, false, errors.New("lock: path must not be empty")
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, false, fmt.Errorf("lock: create directory %s: %w", dir, err)
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, fmt.Errorf("lock: open %s: %w", path, err)
	}

	locked, err := tryFlock(f)
	if err != nil {
		f.Close()
		return nil, false, fmt.Errorf("lock: flock %s: %w", path, err)
	}
	if !locked {
		f.Close()
		return nil, false, nil
	}

	// Best-effort diagnostic content for doctor / manual investigation.
	// Never used for correctness -- a write failure here doesn't fail
	// the lock acquisition itself.
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	_, _ = fmt.Fprintf(f, "pid %d\nsince %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))

	return &Lock{file: f}, true, nil
}

// Release releases the lock and closes the underlying file. The lock file
// itself is left in place (see the package doc for why). Calling Release
// more than once is a safe no-op.
func (l *Lock) Release() error {
	if l == nil || l.released {
		return nil
	}
	l.released = true
	err := unlock(l.file)
	if closeErr := l.file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Status reports whether the lock at path is currently held, without
// blocking and without disturbing a real holder: it attempts a
// non-blocking acquire and immediately releases it if successful. detail
// carries the lock file's best-effort diagnostic content (pid/timestamp)
// when held is true and that content could be read.
func Status(path string) (held bool, detail string, err error) {
	l, ok, err := TryAcquire(path)
	if err != nil {
		return false, "", err
	}
	if ok {
		_ = l.Release()
		return false, "", nil
	}

	if data, readErr := os.ReadFile(path); readErr == nil {
		detail = string(data)
	}
	return true, detail, nil
}
