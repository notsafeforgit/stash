//go:build !windows

package fsutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootRegularFileRejectsFIFOAndPinsOpenIdentity(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600))
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	defer root.Close()
	_, err = OpenRootRegularFile(root, "pipe")
	require.ErrorContains(t, err, "regular file")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "original"), []byte("media"), 0600))
	file, err := OpenRootRegularFile(root, "original")
	require.NoError(t, err)
	defer file.Close()
	identity, err := OpenFileIdentity(file)
	require.NoError(t, err)
	require.NoError(t, os.Rename(filepath.Join(dir, "original"), filepath.Join(dir, "moved")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "original"), []byte("other"), 0600))
	still, err := OpenFileIdentity(file)
	require.NoError(t, err)
	require.Equal(t, identity, still)
	moved, err := FileIdentity(filepath.Join(dir, "moved"))
	require.NoError(t, err)
	require.Equal(t, identity, moved)
	replacement, err := FileIdentity(filepath.Join(dir, "original"))
	require.NoError(t, err)
	require.NotEqual(t, identity, replacement)
}
