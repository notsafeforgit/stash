package file

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stashapp/stash/pkg/fsutil"
)

func completeDeletion(r *deletionRecord, records []*deletionRecord, committed bool, rr RenamerRemover, save func() error) error {
	stageDir := deletionLocation(r.StageDir, records)
	staged := deletionLocation(r.Staged, records)
	parent := deletionLocation(filepath.Dir(r.Original), records)
	if !committed {
		// If a parent restore failed, leave its children staged with it rather
		// than restoring them into a replacement at the parent's original path.
		parent = filepath.Dir(r.Original)
	}
	if !directoryIdentityMatches(parent, r.ParentID) {
		return fmt.Errorf("original parent %q is unavailable or has been replaced", parent)
	}
	if _, err := os.Lstat(stageDir); err == nil {
		if !identityMatches(stageDir, r.StageDirID) {
			return fmt.Errorf("staging directory %q has been replaced", stageDir)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if committed {
		if r.TrashRoot != "" {
			if err := finishTrash(r, staged, rr, save); err != nil {
				return err
			}
		} else if err := removeStaged(r, staged, rr); err != nil {
			return err
		}
	} else {
		if _, err := os.Lstat(staged); errors.Is(err, os.ErrNotExist) {
			// The move never happened, or an earlier recovery already restored it.
			if !identityMatches(r.Original, r.SourceID) {
				return fmt.Errorf("neither %q nor its staged path contains the original file", r.Original)
			}
		} else if err != nil {
			return err
		} else {
			if !identityMatches(staged, r.SourceID) {
				return fmt.Errorf("staged path %q has been replaced", staged)
			}
			if err := rr.Rename(staged, r.Original); err != nil {
				return err
			}
		}
	}

	if err := fsutil.SyncDir(parent); err != nil {
		return err
	}
	if _, err := os.Lstat(stageDir); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := fsutil.SyncDir(stageDir); err != nil {
		return err
	}
	// Never recursively remove the wrapper: unexpected contents are retained.
	if err := rr.Remove(stageDir); err != nil {
		return err
	}
	return fsutil.SyncDir(parent)
}

func removeStaged(r *deletionRecord, staged string, rr RenamerRemover) error {
	if _, err := os.Lstat(staged); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if !identityMatches(staged, r.SourceID) {
		return fmt.Errorf("staged path %q has been replaced", staged)
	}
	if r.Directory {
		return rr.RemoveAll(staged)
	}
	return rr.Remove(staged)
}

func finishTrash(r *deletionRecord, staged string, rr RenamerRemover, save func() error) error {
	if r.TrashRootID != "" && !directoryIdentityMatches(r.TrashRoot, r.TrashRootID) {
		return fmt.Errorf("trash root %q is unavailable or has been replaced", r.TrashRoot)
	}
	if r.Destination != "" {
		if _, err := os.Lstat(filepath.Dir(r.Destination)); errors.Is(err, os.ErrNotExist) && identityMatches(staged, r.SourceID) {
			// A crash may precede persistence of an empty reservation. Allocate
			// a fresh destination; never reuse a missing/replaced directory ID.
			r.Destination, r.TrashDirID, r.CopyDirID, r.CopyID = "", "", "", ""
		} else if err != nil {
			return err
		}
	}
	if r.Destination == "" {
		if err := os.MkdirAll(r.TrashRoot, 0755); err != nil {
			return err
		}
		var err error
		r.TrashRootID, err = fsutil.DirectoryIdentity(r.TrashRoot)
		if err != nil {
			return err
		}
		dir, err := os.MkdirTemp(r.TrashRoot, "stash-trash-")
		if err != nil {
			return err
		}
		r.TrashDirID, err = fsutil.FileIdentity(dir)
		if err != nil {
			_ = os.Remove(dir)
			return err
		}
		r.Destination = filepath.Join(dir, filepath.Base(r.Original))
		if err := fsutil.SyncDir(dir); err != nil {
			_ = os.Remove(dir)
			return err
		}
		// Persist reservations before recording their identities. Otherwise a
		// crash could leave a durable journal referring to a lost empty root.
		for path := r.TrashRoot; ; path = filepath.Dir(path) {
			if err := fsutil.SyncDir(path); err != nil {
				_ = os.Remove(dir)
				return err
			}
			if filepath.Dir(path) == path {
				break
			}
		}
		if err := save(); err != nil {
			_ = os.Remove(dir)
			return err
		}
	}
	trashDir := filepath.Dir(r.Destination)
	if !identityMatches(trashDir, r.TrashDirID) {
		return fmt.Errorf("reserved trash directory %q is unavailable or has been replaced", trashDir)
	}
	wantID := r.SourceID
	if r.CopyID != "" {
		wantID = r.CopyID
	}
	_, destinationErr := os.Lstat(r.Destination)
	switch {
	case destinationErr == nil:
		if !identityMatches(r.Destination, wantID) {
			return fmt.Errorf("trash destination %q is occupied by another file", r.Destination)
		}
	case !errors.Is(destinationErr, os.ErrNotExist):
		return destinationErr
	case r.CopyID != "":
		partial := filepath.Join(trashCopyDir(r), filepath.Base(r.Original))
		if _, err := os.Lstat(partial); errors.Is(err, os.ErrNotExist) && identityMatches(staged, r.SourceID) {
			// The original is intact if an interrupted copy disappeared.
			r.CopyID = ""
			if err := save(); err != nil {
				return err
			}
			if err := copyToTrash(r, staged, save); err != nil {
				return err
			}
		} else if err := publishTrashCopy(r); err != nil {
			return err
		}
	default:
		if !identityMatches(staged, r.SourceID) {
			return fmt.Errorf("original staged file %q is unavailable", staged)
		}
		err := fsutil.RenameNoReplace(staged, r.Destination)
		if fsutil.IsCrossDeviceMove(err) {
			if err := copyToTrash(r, staged, save); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	if err := fsutil.SyncDir(trashDir); err != nil {
		return err
	}
	if r.CopyDirID != "" {
		copyDir := trashCopyDir(r)
		if _, err := os.Lstat(copyDir); err == nil {
			if !identityMatches(copyDir, r.CopyDirID) {
				return fmt.Errorf("trash copy directory %q has been replaced", copyDir)
			}
			if err := os.Remove(copyDir); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := fsutil.SyncDir(trashDir); err != nil {
			return err
		}
	}
	// A completed cross-device copy is durable before its original is removed.
	return removeStaged(r, staged, rr)
}

func trashCopyDir(r *deletionRecord) string {
	name := ".stash-copy"
	if filepath.Base(r.Destination) == name {
		name += "-temp"
	}
	return filepath.Join(filepath.Dir(r.Destination), name)
}

func copyToTrash(r *deletionRecord, staged string, save func() error) error {
	copyDir := trashCopyDir(r)
	if _, err := os.Lstat(copyDir); errors.Is(err, os.ErrNotExist) {
		r.CopyDirID = ""
	}
	if r.CopyDirID == "" {
		// A crash between mkdir and recording its identity can leave an empty
		// reservation. No copy starts before that identity is durable.
		if info, err := os.Lstat(copyDir); err == nil && info.IsDir() {
			if err := os.Remove(copyDir); err != nil {
				return err
			}
		}
		if err := os.Mkdir(copyDir, 0700); err != nil {
			return err
		}
		var err error
		r.CopyDirID, err = fsutil.FileIdentity(copyDir)
		if err != nil {
			return err
		}
		if err := errors.Join(fsutil.SyncDir(copyDir), fsutil.SyncDir(filepath.Dir(copyDir))); err != nil {
			_ = os.Remove(copyDir)
			return err
		}
		if err := save(); err != nil {
			_ = os.Remove(copyDir)
			return err
		}
	}
	if !identityMatches(copyDir, r.CopyDirID) {
		return fmt.Errorf("trash copy directory %q has been replaced", copyDir)
	}
	partial := filepath.Join(copyDir, filepath.Base(r.Original))
	// An incomplete copy is private scratch space. The staged original remains
	// authoritative until CopyID is durably recorded and the copy is published.
	if err := os.RemoveAll(partial); err != nil {
		return err
	}
	if err := fsutil.CopyPathDurable(staged, partial); err != nil {
		return err
	}
	if err := fsutil.SyncDir(copyDir); err != nil {
		return err
	}
	var err error
	r.CopyID, err = fsutil.FileIdentity(partial)
	if err != nil {
		return err
	}
	if err := save(); err != nil {
		return err
	}
	return publishTrashCopy(r)
}

func publishTrashCopy(r *deletionRecord) error {
	copyDir := trashCopyDir(r)
	partial := filepath.Join(copyDir, filepath.Base(r.Original))
	if !identityMatches(copyDir, r.CopyDirID) || !identityMatches(partial, r.CopyID) {
		return fmt.Errorf("completed trash copy %q is unavailable or has been replaced", partial)
	}
	if err := fsutil.RenameNoReplace(partial, r.Destination); err != nil {
		return err
	}
	return errors.Join(fsutil.SyncDir(filepath.Dir(r.Destination)), fsutil.SyncDir(copyDir))
}
