package fsutil

import (
	"errors"
	"os"
	"path/filepath"
)

// WriteFileAtomic publishes a complete, flushed file in the destination
// directory. Existing permissions and symlink targets are preserved.
// PublishedWriteError means rename succeeded but directory durability could not
// be confirmed. Callers must not pretend the previous file is still current.
type PublishedWriteError struct{ Err error }

func (e *PublishedWriteError) Error() string {
	return "file saved, but directory durability could not be confirmed: " + e.Err.Error()
}
func (e *PublishedWriteError) Unwrap() error { return e.Err }
func WasPublished(err error) bool {
	var published *PublishedWriteError
	return errors.As(err, &published)
}

func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	return writeFileAtomic(path, data, mode, SyncDir)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode, syncDirectory func(string) error) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			path, err = filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			info, err = os.Stat(path)
			if err != nil {
				return err
			}
		}
		if !info.Mode().IsRegular() {
			return errors.New("atomic file destination must be a regular file")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".stage-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	if err := syncDirectory(dir); err != nil {
		return &PublishedWriteError{Err: err}
	}
	return nil
}
