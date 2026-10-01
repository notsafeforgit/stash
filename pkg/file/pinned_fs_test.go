package file

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPinnedFileReadersStayIndependentAndConfined(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mp4")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pinned, err := NewPinnedFileFS(ctx, path, f, true)
	require.NoError(t, err)
	first, err := pinned.Open(path)
	require.NoError(t, err)
	defer first.Close()
	second, err := pinned.Open(path)
	require.NoError(t, err)
	defer second.Close()
	b := make([]byte, 3)
	_, err = io.ReadFull(first, b)
	require.NoError(t, err)
	require.Equal(t, "ori", string(b))
	_, err = io.ReadFull(second, b)
	require.NoError(t, err)
	require.Equal(t, "ori", string(b))
	_, err = first.(io.ReadSeeker).Seek(-3, io.SeekEnd)
	require.NoError(t, err)
	_, err = io.ReadFull(first, b)
	require.NoError(t, err)
	require.Equal(t, "nal", string(b))
	require.NoError(t, first.Close())
	_, err = first.Read(b)
	require.ErrorIs(t, err, fs.ErrClosed)
	_, err = f.Stat()
	require.NoError(t, err, "closing a borrowed reader must not close its owner")
	_, err = pinned.Open(path + ".adjacent")
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = pinned.OpenZip(path, 8)
	require.Error(t, err)

	require.NoError(t, os.Rename(path, path+"-old"))
	require.NoError(t, os.WriteFile(path, []byte("replaced"), 0600))
	_, err = second.(io.ReadSeeker).Seek(0, io.SeekStart)
	require.NoError(t, err)
	body, err := io.ReadAll(second)
	require.NoError(t, err)
	require.Equal(t, "original", string(body))
	cancel()
	_, err = second.(io.ReadSeeker).Seek(0, io.SeekStart)
	require.ErrorIs(t, err, context.Canceled)
	_, err = second.Read(b)
	require.ErrorIs(t, err, context.Canceled)
	_, err = second.(io.ReaderAt).ReadAt(b, 0)
	require.ErrorIs(t, err, context.Canceled)
	_, err = pinned.Open(path)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPinnedFileReaderDoesNotConsumeAppendedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mp4")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	require.NoError(t, err)
	defer f.Close()
	pinned, err := NewPinnedFileFS(t.Context(), path, f, true)
	require.NoError(t, err)
	_, err = f.WriteAt([]byte(" appended"), 8)
	require.NoError(t, err)
	r, err := pinned.Open(path)
	require.NoError(t, err)
	defer r.Close()
	body, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "original", string(body))
}
