package file

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

const deleteDirPrefix = ".stash-delete-"

// RenamerRemover provides filesystem operations for staging and deleting paths.
type RenamerRemover interface {
	Renamer
	MkdirTemp(dir, pattern string) (string, error)
	Remove(name string) error
	RemoveAll(path string) error
	Statter
}

type renamerRemoverImpl struct {
	RenameFn    func(oldpath, newpath string) error
	MkdirTempFn func(dir, pattern string) (string, error)
	RemoveFn    func(name string) error
	RemoveAllFn func(path string) error
	StatFn      func(path string) (fs.FileInfo, error)
}

func (r renamerRemoverImpl) Rename(oldpath, newpath string) error {
	return r.RenameFn(oldpath, newpath)
}

func (r renamerRemoverImpl) MkdirTemp(dir, pattern string) (string, error) {
	return r.MkdirTempFn(dir, pattern)
}

func (r renamerRemoverImpl) Remove(name string) error {
	return r.RemoveFn(name)
}

func (r renamerRemoverImpl) RemoveAll(path string) error {
	return r.RemoveAllFn(path)
}

func (r renamerRemoverImpl) Stat(path string) (fs.FileInfo, error) {
	return r.StatFn(path)
}

func newRenamerRemoverImpl() renamerRemoverImpl {
	return renamerRemoverImpl{
		// use fsutil.SafeMove to support cross-device moves
		RenameFn:    fsutil.SafeMove,
		MkdirTempFn: os.MkdirTemp,
		RemoveFn:    os.Remove,
		RemoveAllFn: os.RemoveAll,
		StatFn:      os.Stat,
	}
}

// Deleter stages files on their original filesystem until the database
// transaction finishes. RegisterHooks enables persistent crash recovery when
// the transaction supplies a DeletionJournal.
type Deleter struct {
	RenamerRemover RenamerRemover
	TrashPath      string
	pending        []*deletionRecord
	journal        *DeletionJournal
	stagingErr     error
	registered     bool
}

func newDeletionRenamerRemover() renamerRemoverImpl {
	ret := newRenamerRemoverImpl()
	ret.RenameFn = fsutil.RenameNoReplace
	ret.StatFn = os.Lstat
	return ret
}

func NewDeleter() *Deleter {
	return &Deleter{RenamerRemover: newDeletionRenamerRemover()}
}

func NewDeleterWithTrash(trashPath string) *Deleter {
	ret := NewDeleter()
	ret.TrashPath = trashPath
	return ret
}

// RegisterHooks registers recovery after either transaction outcome. It must
// be called inside the transaction before staging any files.
func (d *Deleter) RegisterHooks(ctx context.Context) {
	if d.registered {
		return
	}
	d.registered = true
	d.journal, _ = ctx.Value(deletionJournalKey{}).(*DeletionJournal)
	txn.AddPreCommitHook(ctx, func(context.Context) error { return d.stagingErr })
	txn.AddPostCommitHook(ctx, func(context.Context) { d.Commit() })
	txn.AddPostRollbackHook(ctx, func(context.Context) { d.Rollback() })
}

// Files stages files for deletion without lengthening their basenames.
// Call Rollback if staging returns an error and hooks are not registered.
func (d *Deleter) Files(paths []string) error {
	return d.stagePaths(paths, false, false)
}

// FileWithValidation stages an existing path in a managed deletion journal and
// validates the actual staged entry before allowing commit. Unlike Files, a
// missing source is an error. A rejected entry remains available to rollback.
func (d *Deleter) FileWithValidation(path string, validate func(staged string) error) error {
	if !d.registered || d.journal == nil || validate == nil {
		d.stagingErr = errors.New("validated deletion requires a managed journal and validator")
		return d.stagingErr
	}
	if err := d.stage(path, false, false); err != nil {
		d.stagingErr = err
		return err
	}
	err := validate(d.pending[len(d.pending)-1].Staged)
	if err != nil {
		d.stagingErr = err
	}
	return err
}

// FilesWithoutTrash permanently deletes generated files after commit.
func (d *Deleter) FilesWithoutTrash(paths []string) error {
	return d.stagePaths(paths, false, true)
}

// Dirs stages directories for deletion, preserving their contents for rollback.
func (d *Deleter) Dirs(paths []string) error {
	return d.stagePaths(paths, true, false)
}

// DirsWithoutTrash permanently deletes generated directories after commit.
func (d *Deleter) DirsWithoutTrash(paths []string) error {
	return d.stagePaths(paths, true, true)
}

// Rollback restores staged paths without overwriting replacement files.
// Failures retain their recovery records and are logged.
func (d *Deleter) Rollback() { d.complete(false) }

// Commit deletes staged paths or transfers them to trash. Completed recovery
// records are removed immediately; failed work remains available for retry.
func (d *Deleter) Commit() { d.complete(true) }

func Destroy(ctx context.Context, destroyer models.FileDestroyer, f models.File, fileDeleter *Deleter, deleteFile bool) error {
	if err := destroyer.Destroy(ctx, f.Base().ID); err != nil {
		return err
	}

	// don't delete files in zip files
	if deleteFile && f.Base().ZipFileID == nil {
		if err := fileDeleter.Files([]string{f.Base().Path}); err != nil {
			return err
		}
	}

	return nil
}

type ZipDestroyer struct {
	FileDestroyer   models.FileFinderDestroyer
	FolderDestroyer models.FolderFinderDestroyer
}

func (d *ZipDestroyer) DestroyZip(ctx context.Context, f models.File, fileDeleter *Deleter, deleteFile bool) error {
	// destroy contained files
	files, err := d.FileDestroyer.FindByZipFileID(ctx, f.Base().ID)
	if err != nil {
		return err
	}

	for _, ff := range files {
		if err := d.FileDestroyer.Destroy(ctx, ff.Base().ID); err != nil {
			return err
		}
	}

	// destroy contained folders
	folders, err := d.FolderDestroyer.FindByZipFileID(ctx, f.Base().ID)
	if err != nil {
		return err
	}

	for _, ff := range folders {
		if err := d.FolderDestroyer.Destroy(ctx, ff.ID); err != nil {
			return err
		}
	}

	if err := d.FileDestroyer.Destroy(ctx, f.Base().ID); err != nil {
		return err
	}

	if deleteFile {
		if err := fileDeleter.Files([]string{f.Base().Path}); err != nil {
			return err
		}
	}

	return nil
}
