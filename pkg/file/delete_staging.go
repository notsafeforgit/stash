package file

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/logger"
)

func (d *Deleter) stagePaths(paths []string, directory, bypassTrash bool) error {
	for _, path := range paths {
		if _, err := d.RenamerRemover.Stat(path); errors.Is(err, os.ErrNotExist) {
			logger.Warnf("Path %q does not exist and therefore cannot be deleted. Ignoring.", path)
			continue
		} else if err != nil {
			d.stagingErr = fmt.Errorf("checking path %q for deletion: %w", path, err)
			return d.stagingErr
		}
		if err := d.stage(path, directory, bypassTrash); err != nil {
			d.stagingErr = fmt.Errorf("staging path %q for deletion: %w", path, err)
			return d.stagingErr
		}
	}
	return nil
}

func (d *Deleter) stage(path string, directory, bypassTrash bool) error {
	original, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(original))
	if err != nil {
		return err
	}
	// Resolve directory aliases, but preserve the final entry if it is a
	// symlink. Nested operations must agree on the same physical parent path.
	original = filepath.Join(parent, filepath.Base(original))
	sourceID, err := fsutil.FileIdentity(original)
	if err != nil {
		return err
	}
	parentID, err := fsutil.DirectoryIdentity(filepath.Dir(original))
	if err != nil {
		return err
	}
	trashRoot := ""
	if d.TrashPath != "" && !bypassTrash {
		trashRoot, err = filepath.Abs(d.TrashPath)
		if err != nil {
			return err
		}
	}
	dir, err := d.RenamerRemover.MkdirTemp(filepath.Dir(original), deleteDirPrefix)
	if err != nil {
		return err
	}
	dirID, err := fsutil.FileIdentity(dir)
	if err != nil {
		_ = d.RenamerRemover.Remove(dir)
		return err
	}
	r := &deletionRecord{
		Version: 1, ID: uuid.NewString(), Original: original,
		StageDir: dir, Staged: filepath.Join(dir, filepath.Base(original)),
		SourceID: sourceID, ParentID: parentID, StageDirID: dirID,
		Directory: directory, TrashRoot: trashRoot,
	}
	if d.journal != nil {
		if err := d.journal.prepare(r); err != nil {
			_ = d.RenamerRemover.Remove(dir)
			return err
		}
	}
	d.pending = append(d.pending, r)
	if err := d.RenamerRemover.Rename(original, r.Staged); err != nil {
		_ = d.RenamerRemover.Remove(dir)
		return err
	}
	if !identityMatches(r.Staged, r.SourceID) {
		return errors.New("source was replaced during deletion staging")
	}
	return errors.Join(fsutil.SyncDir(dir), fsutil.SyncDir(filepath.Dir(original)))
}

func (d *Deleter) complete(committed bool) {
	if d.journal != nil {
		// The persisted transaction outcome is authoritative, including an
		// ambiguous commit error. Recovery runs under the database writer lock.
		if err := d.journal.complete(); err != nil {
			logger.Warnf("File deletion recovery remains pending: %v", err)
		}
		d.pending = nil
		d.journal = nil
	} else {
		sort.SliceStable(d.pending, func(i, j int) bool {
			if committed {
				return len(d.pending[i].Original) > len(d.pending[j].Original)
			}
			return len(d.pending[i].Original) < len(d.pending[j].Original)
		})
		remaining := make(map[string]bool)
		for _, r := range d.pending {
			remaining[r.ID] = true
		}
		var failed []*deletionRecord
		for _, r := range d.pending {
			if committed && hasPendingChild(r, d.pending, remaining) {
				failed = append(failed, r)
				continue
			}
			if err := completeDeletion(r, d.pending, committed, d.RenamerRemover, func() error { return nil }); err != nil {
				logger.Warnf("File deletion remains pending for %q at %q: %v", r.Original, r.Staged, err)
				failed = append(failed, r)
			} else {
				delete(remaining, r.ID)
			}
		}
		d.pending = failed
	}
	d.stagingErr = nil
	d.registered = false
}
