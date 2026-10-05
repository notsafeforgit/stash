//go:build linux || darwin || freebsd || dragonfly

package fsutil

import (
	"math"

	"golang.org/x/sys/unix"
)

// AvailableBytes reports space available to the current user, excluding blocks
// reserved by the filesystem. The caller must use a path on its output volume.
func AvailableBytes(path string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	blocks, size := int64(stat.Bavail), int64(stat.Bsize)
	if blocks <= 0 || size <= 0 {
		return 0, nil
	}
	if blocks > math.MaxInt64/size {
		return math.MaxInt64, nil
	}
	return blocks * size, nil
}
