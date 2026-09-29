package fsutil

import "path/filepath"

// DirectoryIdentity follows a configured directory's symlinks so recovery also
// detects a replaced target or an unavailable filesystem behind a symlink.
func DirectoryIdentity(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return FileIdentity(resolved)
}
