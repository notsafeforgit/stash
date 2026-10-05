package blob

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"

	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/logger"
)

const (
	blobsDirDepth  int = 2
	blobsDirLength int = 2 // thumbDirDepth * thumbDirLength must be smaller than the length of checksum
)

type FSReader interface {
	Open(name string) (fs.ReadDirFile, error)
}

type FSWriter interface {
	WriteFileAtomic(name string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error

	Remove(name string) error

	file.RenamerRemover
}

type FS interface {
	FSReader
	FSWriter
}

type FilesystemReader struct {
	path string
	fs   FSReader
}

func (s *FilesystemReader) checksumToPath(checksum string) string {
	return filepath.Join(s.path, fsutil.GetIntraDir(checksum, blobsDirDepth, blobsDirLength), checksum)
}

func (s *FilesystemReader) Read(ctx context.Context, checksum string) ([]byte, error) {
	if s.path == "" {
		return nil, fmt.Errorf("no path set")
	}

	fn := s.checksumToPath(checksum)
	f, err := s.fs.Open(fn)
	if err != nil {
		return nil, fmt.Errorf("opening file %q: %w", fn, err)
	}

	defer f.Close()

	return io.ReadAll(f)
}

type FilesystemStore struct {
	FilesystemReader
}

func NewFilesystemStore(path string, fs FS) *FilesystemStore {
	return &FilesystemStore{
		FilesystemReader: *NewReadonlyFilesystemStore(path, fs),
	}
}

func NewReadonlyFilesystemStore(path string, fs FSReader) *FilesystemReader {
	return &FilesystemReader{
		path: path,
		fs:   fs,
	}
}

func (s *FilesystemStore) Write(ctx context.Context, checksum string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fs, ok := s.fs.(FS)
	if !ok {
		return fmt.Errorf("internal error: fs is not an FS")
	}

	if s.path == "" {
		return fmt.Errorf("no path set")
	}

	fn := s.checksumToPath(checksum)

	// create the directory if it doesn't exist
	if err := fs.MkdirAll(filepath.Dir(fn), 0755); err != nil {
		return fmt.Errorf("creating directory %q: %w", filepath.Dir(fn), err)
	}

	logger.Debugf("Writing blob file %s", fn)
	// Never truncate an existing inode: a reader or a backup may retain it.
	// Publish a complete, flushed replacement and close its descriptor before
	// the database transaction is allowed to commit.
	if err := fs.WriteFileAtomic(fn, data, 0644); err != nil {
		return fmt.Errorf("publishing file %q: %w", fn, err)
	}

	return nil
}

func (s *FilesystemStore) Delete(ctx context.Context, checksum string) error {
	if s.path == "" {
		return fmt.Errorf("no path set")
	}

	// A deleter belongs to one transaction; sharing it across blob writes can
	// mix a new transaction's paths with the previous transaction's hooks.
	fs, ok := s.fs.(FS)
	if !ok {
		return fmt.Errorf("internal error: fs is not an FS")
	}
	deleter := &file.Deleter{RenamerRemover: fs}
	deleter.RegisterHooks(ctx)

	fn := s.checksumToPath(checksum)

	if err := deleter.Files([]string{fn}); err != nil {
		return fmt.Errorf("deleting file %q: %w", fn, err)
	}

	return nil
}
