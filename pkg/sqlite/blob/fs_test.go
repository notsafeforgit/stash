package blob

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stretchr/testify/require"
)

func TestFilesystemWritePreservesPinnedInode(t *testing.T) {
	s := NewFilesystemStore(t.TempDir(), &file.OsFS{})
	data := []byte("original artwork")
	checksum := md5.FromBytes(data)
	path := s.checksumToPath(checksum)
	require.NoError(t, s.Write(t.Context(), checksum, data))
	// Simulate repairing a damaged on-disk copy. Even a repair must not mutate
	// the inode retained by a backup or an already-open reader.
	require.NoError(t, os.WriteFile(path, []byte("damaged artwork"), 0600))
	pinned := filepath.Join(t.TempDir(), "pinned")
	require.NoError(t, os.Link(path, pinned))
	reader, err := os.Open(path)
	require.NoError(t, err)
	defer reader.Close()
	before, err := reader.Stat()
	require.NoError(t, err)
	require.NoError(t, s.Write(t.Context(), checksum, data))
	after, err := os.Stat(path)
	require.NoError(t, err)
	require.False(t, os.SameFile(before, after))
	got, err := s.Read(t.Context(), checksum)
	require.NoError(t, err)
	require.Equal(t, data, got)
	got, err = os.ReadFile(pinned)
	require.NoError(t, err)
	require.Equal(t, "damaged artwork", string(got))
	// Unlinking the current live artwork does not remove the retained copy.
	require.NoError(t, os.Remove(path))
	got, err = os.ReadFile(pinned)
	require.NoError(t, err)
	require.Equal(t, "damaged artwork", string(got))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestFilesystemFailedWritePreservesDestination(t *testing.T) {
	s := NewFilesystemStore(t.TempDir(), &file.OsFS{})
	data := []byte("artwork")
	checksum := md5.FromBytes(data)
	path := s.checksumToPath(checksum)
	require.NoError(t, os.MkdirAll(path, 0700))
	keep := filepath.Join(path, "keep")
	require.NoError(t, os.WriteFile(keep, []byte("keep"), 0600))
	require.Error(t, s.Write(t.Context(), checksum, data))
	got, err := os.ReadFile(keep)
	require.NoError(t, err)
	require.Equal(t, "keep", string(got))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.NoError(t, os.RemoveAll(path))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, s.Write(ctx, checksum, data), context.Canceled)
	require.NoFileExists(t, path)
}
