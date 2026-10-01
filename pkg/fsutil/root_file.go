package fsutil

import (
	"errors"
	"os"
)

// OpenRootRegularFile checks before and after opening. On Unix a nonblocking
// open also prevents a replacement FIFO from hanging the caller between those
// checks. The returned descriptor stays within os.Root's path confinement.
func OpenRootRegularFile(root *os.Root, name string) (*os.File, error) {
	info, err := root.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("media path is not a regular file")
	}
	file, err := root.OpenFile(name, rootReadOnlyFlags, 0)
	if err != nil {
		return nil, err
	}
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err == nil {
			err = errors.New("media path is not a regular file")
		}
		return nil, err
	}
	return file, nil
}
