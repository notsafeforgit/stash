package fsutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CopyPathDurable copies a file, symlink, or directory into an unused private
// destination. The caller owns partial output on error. Every copied file and
// directory is flushed before the caller publishes the completed copy.
func CopyPathDurable(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(target, destination)
	case info.IsDir():
		if err := os.Mkdir(destination, 0700); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := CopyPathDurable(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
				return err
			}
		}
		if err := os.Chmod(destination, info.Mode().Perm()); err != nil {
			return err
		}
		if err := os.Chtimes(destination, info.ModTime(), info.ModTime()); err != nil {
			return err
		}
		return SyncDir(destination)
	case info.Mode().IsRegular():
		return copyRegularFileDurable(source, destination, info)
	default:
		return fmt.Errorf("cannot copy special file %q to trash", source)
	}
}

func copyRegularFileDurable(source, destination string, before os.FileInfo) (retErr error) {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, out.Close()) }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	after, err := in.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("source changed while copying %q to trash", source)
	}
	if err := out.Chmod(before.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Chtimes(destination, before.ModTime(), before.ModTime()); err != nil {
		return err
	}
	return out.Sync()
}
