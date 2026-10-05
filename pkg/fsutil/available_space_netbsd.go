package fsutil

import (
	"math"

	"golang.org/x/sys/unix"
)

func AvailableBytes(path string) (int64, error) {
	var stat unix.Statvfs_t
	if err := unix.Statvfs(path, &stat); err != nil {
		return 0, err
	}
	if stat.Frsize == 0 {
		return 0, nil
	}
	if stat.Bavail > math.MaxInt64/stat.Frsize {
		return math.MaxInt64, nil
	}
	return int64(stat.Bavail * stat.Frsize), nil
}
