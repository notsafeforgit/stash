//go:build !linux && !darwin && !windows

package fsutil

import "errors"

func renameNoReplace(oldpath, newpath string) error {
	return errors.New("atomic rename without replacement is unsupported on this platform")
}
