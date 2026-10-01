//go:build !windows

package fsutil

import (
	"fmt"
	"os"
	"syscall"
)

const rootReadOnlyFlags = os.O_RDONLY | syscall.O_NONBLOCK

// FileIdentity identifies a directory entry across renames, without following
// a final symlink. Recovery uses it to avoid touching replacement files.
func FileIdentity(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("file identity unavailable for %q", path)
	}
	return fmt.Sprintf("%x:%x", stat.Dev, stat.Ino), nil
}

// OpenFileIdentity uses the already-open descriptor, avoiding a path lookup
// that could observe a replacement directory or file.
func OpenFileIdentity(file *os.File) (string, error) {
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("opened file identity unavailable")
	}
	return fmt.Sprintf("%x:%x", stat.Dev, stat.Ino), nil
}
