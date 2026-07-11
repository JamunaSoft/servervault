package lock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire(): unexpected error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Acquire() did not create the lock file: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Errorf("Release(): unexpected error: %v", err)
	}

	// The lock file itself must survive release -- it is never deleted.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file was removed on Release(): %v", err)
	}
}

func TestRelease_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire(): unexpected error: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("first Release(): unexpected error: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Errorf("second Release(): want a safe no-op, got error: %v", err)
	}
}

func TestAcquire_SecondCallerIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")

	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire() (first): unexpected error: %v", err)
	}
	defer first.Release()

	_, err = Acquire(path)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("Acquire() (second, concurrent): error = %v, want ErrLocked", err)
	}
}

func TestAcquire_AvailableAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")

	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire() (first): unexpected error: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release(): unexpected error: %v", err)
	}

	second, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire() (second, after release): unexpected error: %v", err)
	}
	defer second.Release()
}

func TestAcquire_CreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "test.lock")

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire(): unexpected error: %v", err)
	}
	defer l.Release()

	if _, err := os.Stat(path); err != nil {
		t.Errorf("Acquire() did not create nested parent directories: %v", err)
	}
}

func TestAcquire_EmptyPath(t *testing.T) {
	if _, err := Acquire(""); err == nil {
		t.Fatal("Acquire(\"\"): want an error, got nil")
	}
}

func TestTryAcquire_ReportsBusyWithoutError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")

	first, ok, err := TryAcquire(path)
	if err != nil || !ok {
		t.Fatalf("TryAcquire() (first): ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	defer first.Release()

	_, ok, err = TryAcquire(path)
	if err != nil {
		t.Fatalf("TryAcquire() (second, busy): unexpected error: %v", err)
	}
	if ok {
		t.Error("TryAcquire() (second, busy): ok = true, want false")
	}
}

func TestStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")

	held, _, err := Status(path)
	if err != nil {
		t.Fatalf("Status() before any lock exists: unexpected error: %v", err)
	}
	if held {
		t.Error("Status() before any lock exists: held = true, want false")
	}

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire(): unexpected error: %v", err)
	}
	defer l.Release()

	held, detail, err := Status(path)
	if err != nil {
		t.Fatalf("Status() while held: unexpected error: %v", err)
	}
	if !held {
		t.Error("Status() while held: held = false, want true")
	}
	if !strings.Contains(detail, "pid") {
		t.Errorf("Status() detail = %q, want it to contain diagnostic pid info", detail)
	}
}

func TestStatus_DoesNotDisruptARealHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire(): unexpected error: %v", err)
	}
	defer l.Release()

	// Calling Status() repeatedly while held must never itself acquire
	// the lock out from under the real holder.
	for i := 0; i < 3; i++ {
		held, _, err := Status(path)
		if err != nil {
			t.Fatalf("Status() iteration %d: unexpected error: %v", i, err)
		}
		if !held {
			t.Fatalf("Status() iteration %d: held = false, want true (real holder still holds it)", i)
		}
	}

	// The real holder must still be able to release cleanly afterward.
	if err := l.Release(); err != nil {
		t.Errorf("Release() after repeated Status() probes: unexpected error: %v", err)
	}
}
