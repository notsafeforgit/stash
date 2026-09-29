//go:build !windows

package fsutil

import (
	"fmt"
	"os"
	"syscall"
)

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
