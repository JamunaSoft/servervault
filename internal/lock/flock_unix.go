//go:build unix

package lock

import (
	"os"
	"syscall"
)

// tryFlock attempts a non-blocking exclusive flock on f, reporting
// (true, nil) if acquired, (false, nil) if another process already holds
// it, or a non-nil error for anything else.
func tryFlock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if err == syscall.EWOULDBLOCK {
		return false, nil
	}
	return false, err
}

func unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
