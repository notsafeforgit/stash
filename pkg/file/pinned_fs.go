package file

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/stashapp/stash/pkg/models"
)

// OpenedFileProvider lends a descriptor for the duration of a synchronous probe.
// The borrower must not close it. Its owner keeps it open through validation and
// commit. Other FS implementations need not support this capability.
type OpenedFileProvider interface {
	BorrowFile(string) (*os.File, error)
}

// PinnedFileFS provides independent readers of one already-open media file.
// No operation follows the source pathname or grants access to adjacent files.
type PinnedFileFS struct {
	ctx           context.Context
	path          string
	file          *os.File
	size          int64
	caseSensitive bool
}

func NewPinnedFileFS(ctx context.Context, path string, file *os.File, caseSensitive bool) (*PinnedFileFS, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) || !info.Mode().IsRegular() {
		return nil, errors.New("pinned media requires an absolute name and regular descriptor")
	}
	return &PinnedFileFS{ctx: ctx, path: path, file: file, size: info.Size(), caseSensitive: caseSensitive}, nil
}

func (p *PinnedFileFS) BorrowFile(name string) (*os.File, error) {
	if err := p.ctx.Err(); err != nil {
		return nil, err
	}
	if name != p.path {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return p.file, nil
}

func (p *PinnedFileFS) Stat(name string) (fs.FileInfo, error) {
	f, err := p.BorrowFile(name)
	if err != nil {
		return nil, err
	}
	return f.Stat()
}
func (p *PinnedFileFS) Lstat(name string) (fs.FileInfo, error) { return p.Stat(name) }

type pinnedReader struct {
	*io.SectionReader
	ctx    context.Context
	file   *os.File
	closed bool
}

func (r *pinnedReader) check() error {
	if r.closed {
		return fs.ErrClosed
	}
	return r.ctx.Err()
}
func (r *pinnedReader) Read(p []byte) (int, error) {
	if err := r.check(); err != nil {
		return 0, err
	}
	return r.SectionReader.Read(p)
}
func (r *pinnedReader) ReadAt(p []byte, offset int64) (int, error) {
	if err := r.check(); err != nil {
		return 0, err
	}
	return r.SectionReader.ReadAt(p, offset)
}
func (r *pinnedReader) Seek(offset int64, whence int) (int64, error) {
	if err := r.check(); err != nil {
		return 0, err
	}
	return r.SectionReader.Seek(offset, whence)
}
func (r *pinnedReader) Stat() (fs.FileInfo, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	return r.file.Stat()
}
func (r *pinnedReader) Close() error                         { r.closed = true; return nil }
func (r *pinnedReader) ReadDir(n int) ([]fs.DirEntry, error) { return nil, fs.ErrInvalid }

func (p *PinnedFileFS) Open(name string) (fs.ReadDirFile, error) {
	f, err := p.BorrowFile(name)
	if err != nil {
		return nil, err
	}
	return &pinnedReader{SectionReader: io.NewSectionReader(f, 0, p.size), file: f, ctx: p.ctx}, nil
}
func (p *PinnedFileFS) OpenZip(name string, size int64) (models.ZipFS, error) {
	return nil, errors.New("pinned media intake does not accept archives")
}
func (p *PinnedFileFS) IsPathCaseSensitive(name string) (bool, error) {
	if _, err := p.BorrowFile(name); err != nil {
		return false, err
	}
	return p.caseSensitive, nil
}
