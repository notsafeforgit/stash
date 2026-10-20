package file

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type Renamer interface {
	Rename(oldpath, newpath string) error
}

type Statter interface {
	Stat(name string) (fs.FileInfo, error)
}

type DirMakerStatRenamer interface {
	Statter
	Renamer
	Mkdir(name string, perm os.FileMode) error
	Remove(name string) error
}

type folderCreatorStatRenamerImpl struct {
	renamerRemoverImpl
	mkDirFn func(name string, perm os.FileMode) error
}

func (r folderCreatorStatRenamerImpl) Mkdir(name string, perm os.FileMode) error {
	return r.mkDirFn(name, perm)
}

type Mover struct {
	Renamer DirMakerStatRenamer
	Files   models.FileFinderUpdater
	Folders models.FolderReaderWriter

	foldersCreated []moveDirectory
	createdParent  moveDirectory
	journal        *MoveJournal
	err            error
	completionErr  error
	registered     bool
	effectsStarted bool

	// needed for creating folder hierarchy when moving zip file entries
	rootPaths []string
}

func NewMover(fileStore models.FileFinderUpdater, folderStore models.FolderReaderWriter, rootPaths []string) *Mover {
	return &Mover{
		Files:   fileStore,
		Folders: folderStore,
		Renamer: &folderCreatorStatRenamerImpl{
			renamerRemoverImpl: newDeletionRenamerRemover(),
			mkDirFn:            os.Mkdir,
		},
		rootPaths: rootPaths,
	}
}

// Move moves the file to the given folder and basename. If basename is empty, then the existing basename is used.
// Assumes that the parent folder exists in the filesystem.
// The caller must keep the transaction's writer lock until this call returns,
// even on cancellation (use txn.WithTxnUntilComplete).
func (m *Mover) Move(ctx context.Context, f models.File, folder *models.Folder, basename string) (retErr error) {
	defer func() {
		if retErr != nil {
			m.err = retErr
		}
	}()
	if m.err != nil {
		return m.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if f == nil || folder == nil {
		return errors.New("file move requires an existing file and folder")
	}
	fBase := f.Base()

	// don't allow moving files in zip files
	if fBase.ZipFileID != nil {
		return fmt.Errorf("cannot move file %s, is in a zip file", fBase.Path)
	}

	if basename == "" {
		basename = fBase.Basename
	}
	if err := ValidateMoveBasename(basename); err != nil {
		return err
	}
	if folder.ZipFileID != nil {
		return errors.New("cannot move a file into an archive")
	}
	if _, err := managedMovePath(filepath.Dir(fBase.Path), m.rootPaths); err != nil {
		return err
	}
	if _, err := managedMovePath(folder.Path, m.rootPaths); err != nil {
		return err
	}
	if !m.registered || m.journal == nil {
		return errors.New("file moves require a managed recovery journal")
	}

	// modify the database first

	oldPath := fBase.Path

	if folder.ID == fBase.ParentFolderID && (basename == "" || basename == fBase.Basename) {
		// nothing to do
		return nil
	}

	// ensure that the new path doesn't already exist
	newPath := filepath.Join(folder.Path, basename)
	if _, err := m.Renamer.Stat(newPath); !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("file %s already exists", newPath)
	}

	zipMover := zipHierarchyMover{
		folderStore: m.Folders,
		files:       m.Files,
		rootPaths:   m.rootPaths,
	}

	if err := zipMover.transferZipHierarchy(ctx, fBase.ID, oldPath, newPath); err != nil {
		return fmt.Errorf("moving folder hierarchy for file %s: %w", fBase.Path, err)
	}

	fBase.ParentFolderID = folder.ID
	fBase.Basename = basename
	fBase.UpdatedAt = time.Now()
	// leave ModTime as is. It may or may not be changed by this operation

	if err := m.Files.Update(ctx, f); err != nil {
		return fmt.Errorf("updating file %s: %w", oldPath, err)
	}

	// then move the file
	return m.moveFile(ctx, fBase.ID, oldPath, newPath)
}

func (m *Mover) CreateFolderHierarchy(path string) (retErr error) {
	defer func() {
		if retErr != nil {
			m.err = retErr
		}
	}()
	if !m.registered || m.journal == nil {
		return errors.New("file moves require a managed recovery journal")
	}
	path, err := managedMovePath(path, m.rootPaths)
	if err != nil {
		return err
	}
	return m.createFolderHierarchy(path)
}

func (m *Mover) createFolderHierarchy(path string) error {
	info, err := m.Renamer.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// create the parent folder
			parentPath := filepath.Dir(path)
			if err := m.createFolderHierarchy(parentPath); err != nil {
				return err
			}

			// create the folder
			if len(m.foldersCreated) == 0 {
				id, err := fsutil.DirectoryIdentity(parentPath)
				if err != nil {
					return err
				}
				m.createdParent = moveDirectory{Path: parentPath, ID: id}
			}
			if err := m.Renamer.Mkdir(path, 0755); err != nil {
				return fmt.Errorf("creating folder %s: %w", path, err)
			}

			id, err := fsutil.DirectoryIdentity(path)
			if err != nil {
				return err
			}
			m.foldersCreated = append(m.foldersCreated, moveDirectory{Path: path, ID: id})
			if err := fsutil.SyncDir(parentPath); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("getting info for %s: %w", path, err)
		}
	} else {
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", path)
		}
	}

	return nil
}

func (m *Mover) moveFile(ctx context.Context, fileID models.FileID, oldPath, newPath string) error {
	sourceParent, err := managedMovePath(filepath.Dir(oldPath), m.rootPaths)
	if err != nil {
		return err
	}
	destinationParent, err := managedMovePath(filepath.Dir(newPath), m.rootPaths)
	if err != nil {
		return err
	}
	oldPath, newPath = filepath.Join(sourceParent, filepath.Base(oldPath)), filepath.Join(destinationParent, filepath.Base(newPath))
	info, err := os.Lstat(oldPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("only regular files can be moved; symlinks and special files are not followed")
	}
	sourceID, err := fsutil.FileIdentity(oldPath)
	if err != nil {
		return err
	}
	parentID, err := fsutil.DirectoryIdentity(sourceParent)
	if err != nil {
		return err
	}
	destinationID, err := fsutil.DirectoryIdentity(destinationParent)
	if err != nil {
		return err
	}
	stage, err := newMoveDirectory(sourceParent)
	if err != nil {
		return err
	}
	r := &moveRecord{
		Version: 1, ID: uuid.NewString(), FileID: fileID, Original: oldPath, Destination: newPath, SourceID: sourceID,
		SourceParent:      moveDirectory{Path: sourceParent, ID: parentID},
		DestinationParent: moveDirectory{Path: destinationParent, ID: destinationID},
		Stage:             stage, Created: append([]moveDirectory(nil), m.foldersCreated...),
		CreatedParent: m.createdParent,
	}
	if err := m.journal.prepare(r); err != nil {
		_ = os.Remove(stage.Path)
		return err
	}
	m.effectsStarted = true
	if err := m.Renamer.Rename(oldPath, r.staged()); err != nil {
		return err
	}
	if !identityMatches(r.staged(), r.SourceID) {
		return errors.New("file move source was replaced during staging")
	}
	if err := errors.Join(fsutil.SyncDir(stage.Path), fsutil.SyncDir(sourceParent)); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	err = m.Renamer.Rename(r.staged(), newPath)
	switch {
	case fsutil.IsCrossDeviceMove(err):
		if err := m.copyMove(ctx, r); err != nil {
			return err
		}
	case err != nil:
		return fmt.Errorf("renaming file %s to %s: %w", oldPath, newPath, err)
	case !identityMatches(newPath, r.SourceID):
		return errors.New("file move destination was replaced during publication")
	}
	return errors.Join(ctx.Err(), fsutil.SyncDir(stage.Path), syncMoveParents(r))
}

func newMoveDirectory(parent string) (moveDirectory, error) {
	path, err := os.MkdirTemp(parent, moveDirPrefix)
	if err != nil {
		return moveDirectory{}, err
	}
	id, err := fsutil.DirectoryIdentity(path)
	if err == nil {
		err = fsutil.SyncDir(parent)
	}
	if err != nil {
		_ = os.Remove(path)
		return moveDirectory{}, err
	}
	return moveDirectory{Path: path, ID: id}, nil
}

func (m *Mover) copyMove(ctx context.Context, r *moveRecord) error {
	dir, err := newMoveDirectory(r.DestinationParent.Path)
	if err != nil {
		return err
	}
	r.Copy = dir
	if err := writeMoveRecord(m.journal.Directory, r); err != nil {
		_ = os.Remove(dir.Path)
		return err
	}
	if err := fsutil.CopyPathDurableContext(ctx, r.staged(), r.copied()); err != nil {
		return err
	}
	if !identityMatches(r.staged(), r.SourceID) {
		return errors.New("file move source was replaced while copying")
	}
	r.CopyID, err = fsutil.FileIdentity(r.copied())
	if err != nil {
		return err
	}
	if err := fsutil.SyncDir(r.Copy.Path); err != nil {
		return err
	}
	if err := writeMoveRecord(m.journal.Directory, r); err != nil {
		return err
	}
	if err := m.Renamer.Rename(r.copied(), r.Destination); err != nil {
		return err
	}
	if !identityMatches(r.Destination, r.CopyID) {
		return errors.New("file move copy was replaced during publication")
	}
	return fsutil.SyncDir(r.Copy.Path)
}

func (m *Mover) RegisterHooks(ctx context.Context) {
	if m.registered || !txn.HasHooks(ctx) {
		return
	}
	m.registered = true
	m.journal, _ = ctx.Value(moveJournalKey{}).(*MoveJournal)
	txn.AddPreCommitHook(ctx, func(context.Context) error { return m.err })
	txn.AddPostCompleteHook(ctx, func(context.Context) {
		if m.journal != nil {
			m.completionErr = m.journal.complete()
			if m.completionErr != nil {
				logger.Errorf("File move recovery remains pending: %v", m.completionErr)
			}
		}
	})
	txn.AddPostRollbackHook(ctx, func(context.Context) {
		// Journal recovery handles directories once a move has been staged.
		// Earlier validation failures may have created only empty directories.
		if !m.effectsStarted {
			for i := len(m.foldersCreated) - 1; i >= 0; i-- {
				dir := m.foldersCreated[i]
				if directoryIdentityMatches(dir.Path, dir.ID) {
					_ = os.Remove(dir.Path)
					_ = fsutil.SyncDir(filepath.Dir(dir.Path))
				}
			}
		}
	})
}

// CompletionError reports retained recovery work after the transaction ends.
func (m *Mover) CompletionError() error {
	return m.completionErr
}

// correctSubFolderHierarchy sets the path of all contained folders to be relative to the given folder.
// It does not move the folder hierarchy in the filesystem.
func correctSubFolderHierarchy(ctx context.Context, rw models.FolderReaderWriter, folder *models.Folder) error {
	folders, err := rw.FindByParentFolderID(ctx, folder.ID)
	if err != nil {
		return fmt.Errorf("finding contained folders in folder %s: %w", folder.Path, err)
	}

	folderPath := folder.Path

	for _, f := range folders {
		oldPath := f.Path
		folderBasename := filepath.Base(f.Path)
		correctPath := filepath.Join(folderPath, folderBasename)

		logger.Debugf("updating folder %s to %s", oldPath, correctPath)

		// #6427 - ensure folder entry with new path doesn't already exist
		const caseSensitive = true
		existing, err := rw.FindByPath(ctx, correctPath, caseSensitive)
		if err != nil {
			return fmt.Errorf("finding folder by path %s: %w", correctPath, err)
		}

		if existing != nil {
			// this should no longer be possible, but if it does happen, log a warning
			// and skip updating this folder and its subfolders
			logger.Warnf("folder with path %s already exists, setting parent_folder_id of %s to NULL and skipping", correctPath, oldPath)
			f.ParentFolderID = nil
			if err := rw.Update(ctx, f); err != nil {
				return fmt.Errorf("updating folder parent id to NULL for folder %s: %w", oldPath, err)
			}

			continue
		}

		f.Path = correctPath
		if err := rw.Update(ctx, f); err != nil {
			return fmt.Errorf("updating folder path %s -> %s: %w", oldPath, f.Path, err)
		}

		// recurse
		if err := correctSubFolderHierarchy(ctx, rw, f); err != nil {
			return err
		}
	}

	return nil
}
