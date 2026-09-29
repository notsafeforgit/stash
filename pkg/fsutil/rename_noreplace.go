package fsutil

import "os"

// RenameNoReplace moves a path without copying it or replacing a destination.
// An unsupported filesystem fails safely instead of using a check-then-rename.
func RenameNoReplace(oldpath, newpath string) error {
	if err := renameNoReplace(oldpath, newpath); err != nil {
		return &os.LinkError{Op: "rename without replacement", Old: oldpath, New: newpath, Err: err}
	}
	return nil
}

// SyncDir persists directory entry changes before a recovery record is removed.
func SyncDir(path string) error {
	return syncDir(path)
}

func IsCrossDeviceMove(err error) bool {
	return isCrossDeviceMove(err)
}
