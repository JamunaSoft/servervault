//go:build linux

package doctor

import "syscall"

// freeBytes reports the free space available to an unprivileged user on
// the filesystem containing path, using statfs(2).
func freeBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bsize) * stat.Bavail, nil
}
