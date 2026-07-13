//go:build !linux

package doctor

import (
	"fmt"
	"runtime"
)

// freeBytes is not implemented outside Linux; ServerVault's supported
// environment is Ubuntu/Debian (see docs/deployment.md). The disk space
// check reports StatusSkip rather than a false OK/FAIL on other platforms.
func freeBytes(path string) (uint64, error) {
	return 0, fmt.Errorf("disk space check is not implemented on %s", runtime.GOOS)
}
