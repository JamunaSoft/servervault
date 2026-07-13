//go:build !unix

package lock

import (
	"fmt"
	"os"
	"runtime"
)

// tryFlock is not implemented outside unix platforms; ServerVault's
// supported environment is Ubuntu/Debian (see docs/deployment.md).
func tryFlock(f *os.File) (bool, error) {
	return false, fmt.Errorf("lock: flock is not implemented on %s", runtime.GOOS)
}

func unlock(f *os.File) error {
	return fmt.Errorf("lock: flock is not implemented on %s", runtime.GOOS)
}
