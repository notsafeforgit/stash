package file

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stashapp/stash/pkg/fsutil"
)

func checkMoveDirectory(dir moveDirectory, optional bool) error {
	if optional {
		if _, err := os.Lstat(dir.Path); errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	if !directoryIdentityMatches(dir.Path, dir.ID) {
		return fmt.Errorf("file move directory %q is unavailable or replaced", dir.Path)
	}
	return nil
}

func syncMoveParents(r *moveRecord) error {
	return errors.Join(fsutil.SyncDir(r.SourceParent.Path), fsutil.SyncDir(r.DestinationParent.Path))
}

func completeMove(r *moveRecord, committed bool) error {
	if len(r.Created) > 0 {
		if err := checkMoveDirectory(r.CreatedParent, false); err != nil {
			return err
		}
	}
	if !committed && identityMatches(r.Original, r.SourceID) {
		// Directory removal is also restartable. A previous recovery may have
		// restored the file and removed the newly created destination parent
		// immediately before the journal could be pruned.
		if _, err := os.Lstat(r.DestinationParent.Path); errors.Is(err, os.ErrNotExist) {
			for _, dir := range r.Created {
				if dir.Path == r.DestinationParent.Path {
					if err := checkMoveDirectory(r.Stage, true); err != nil {
						return err
					}
					if err := os.Remove(r.Stage.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
						return err
					}
					if err := fsutil.SyncDir(r.SourceParent.Path); err != nil {
						return err
					}
					return nil
				}
			}
		}
	}
	for _, dir := range []moveDirectory{r.SourceParent, r.DestinationParent} {
		if err := checkMoveDirectory(dir, false); err != nil {
			return err
		}
	}
	if err := checkMoveDirectory(r.Stage, true); err != nil {
		return err
	}
	if r.Copy.Path != "" {
		if err := checkMoveDirectory(r.Copy, true); err != nil {
			return err
		}
	}
	destinationID := r.SourceID
	if r.CopyID != "" {
		destinationID = r.CopyID
	}
	if committed {
		if !identityMatches(r.Destination, destinationID) {
			return errors.New("committed move destination is unavailable or replaced")
		}
		if r.CopyID != "" {
			if err := removeMoveFile(r.staged(), r.SourceID); err != nil {
				return err
			}
		} else if _, err := os.Lstat(r.staged()); !errors.Is(err, os.ErrNotExist) {
			return errors.New("committed rename still has a staged source")
		}
	} else {
		// Remove only our published cross-device copy. Keep the original until
		// it has been restored; an unrelated destination is never removed.
		if r.CopyID != "" && identityMatches(r.Destination, r.CopyID) {
			if err := removeMoveFile(r.Destination, r.CopyID); err != nil {
				return err
			}
		}
		switch {
		case identityMatches(r.Original, r.SourceID):
			// Never staged, or an interrupted recovery already restored it.
		case identityMatches(r.staged(), r.SourceID):
			if err := fsutil.RenameNoReplace(r.staged(), r.Original); err != nil {
				return err
			}
		case r.CopyID == "" && identityMatches(r.Destination, r.SourceID):
			if err := fsutil.RenameNoReplace(r.Destination, r.Original); err != nil {
				return err
			}
		default:
			return errors.New("original move source is unavailable or replaced")
		}
	}
	if err := syncMoveParents(r); err != nil {
		return err
	}
	if r.Copy.Path != "" {
		if _, err := os.Lstat(r.Copy.Path); err == nil {
			// Only this payload name belongs to the copy operation. Unexpected
			// siblings leave the private directory and record pending.
			if r.CopyID != "" {
				if err := removeMoveFile(r.copied(), r.CopyID); err != nil {
					return err
				}
			} else if err := os.Remove(r.copied()); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err := fsutil.SyncDir(r.Copy.Path); err != nil {
				return err
			}
			if err := os.Remove(r.Copy.Path); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if _, err := os.Lstat(r.Stage.Path); err == nil {
		if err := fsutil.SyncDir(r.Stage.Path); err != nil {
			return err
		}
		if err := os.Remove(r.Stage.Path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncMoveParents(r); err != nil {
		return err
	}
	return nil
}

func removeCreatedMoveDirectories(r *moveRecord) error {
	for i := len(r.Created) - 1; i >= 0; i-- {
		dir := r.Created[i]
		if _, err := os.Lstat(dir.Path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := checkMoveDirectory(dir, false); err != nil {
			return err
		}
		if err := os.Remove(dir.Path); err != nil {
			return err
		}
		if err := fsutil.SyncDir(filepath.Dir(dir.Path)); err != nil {
			return err
		}
	}
	return nil
}

func removeMoveFile(path, expected string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if !identityMatches(path, expected) {
		return fmt.Errorf("file move path %q was replaced", path)
	}
	return os.Remove(path)
}
