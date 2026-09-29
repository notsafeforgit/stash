//go:build !windows

package fsutil

import (
	"errors"
	"os"
	"syscall"
)

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func isCrossDeviceMove(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}
