package fsutil

import (
	"errors"

	"golang.org/x/sys/windows"
)

func renameNoReplace(oldpath, newpath string) error {
	from, err := windows.UTF16PtrFromString(oldpath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(newpath)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

func syncDir(path string) error {
	// Windows does not expose POSIX directory fsync. Renames use WRITE_THROUGH;
	// journal files and copied payloads are explicitly flushed before publishing.
	return nil
}

func isCrossDeviceMove(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}
