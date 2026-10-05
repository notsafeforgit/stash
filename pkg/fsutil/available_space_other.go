//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package fsutil

import "errors"

func AvailableBytes(path string) (int64, error) {
	return 0, errors.New("available disk space reporting is unsupported on this platform")
}
