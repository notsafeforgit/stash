package fsutil

import (
	"math"

	"golang.org/x/sys/unix"
)

func AvailableBytes(path string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	blocks, size := stat.F_bavail, int64(stat.F_bsize)
	if blocks <= 0 || size <= 0 {
		return 0, nil
	}
	if blocks > math.MaxInt64/size {
		return math.MaxInt64, nil
	}
	return blocks * size, nil
}
