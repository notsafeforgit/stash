package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomicPreservesDestinationAndCleansStaging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	require.NoError(t, WriteFileAtomic(path, []byte("original"), 0600))
	require.NoError(t, WriteFileAtomic(path, []byte("replacement"), 0644))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Error(t, WriteFileAtomic(dir, []byte("invalid"), 0600))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(data))
}

func TestWriteFileAtomicPreservesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks can require administrator privileges")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.yml")
	link := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(target, []byte("original"), 0600))
	require.NoError(t, os.Symlink("real.yml", link))
	require.NoError(t, WriteFileAtomic(link, []byte("replacement"), 0644))
	got, err := os.Readlink(link)
	require.NoError(t, err)
	require.Equal(t, "real.yml", got)
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(data))
}

func TestAtomicWriteReportsPostRenameDurabilityFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	failure := errors.New("directory fsync failed")
	err := writeFileAtomic(path, []byte("published"), 0600, func(string) error { return failure })
	require.ErrorIs(t, err, failure)
	require.True(t, WasPublished(err))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "published", string(data))
}
