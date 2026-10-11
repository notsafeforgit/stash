package file

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/stashapp/stash/pkg/fsutil"
)

// Independent permanent deletions can share their parent's durability barrier.
// A removed staging wrapper needs no separate flush: syncing its parent makes
// the removal durable. Nested paths and trash transfers keep their established
// recovery ordering and reservation protocol.
func recoverDeletionGroups(records []*deletionRecord, committed map[string]bool, unknown bool) map[string]error {
	groups := map[string][]*deletionRecord{}
	for _, r := range records {
		if !committed[r.ID] || r.TrashRoot != "" || (r.Directory && unknown) {
			continue
		}
		nested := false
		for _, other := range records {
			if r == other {
				continue
			}
			if r.Original == other.Original || r.StageDir == other.StageDir ||
				(other.Directory && strings.HasPrefix(r.Original, other.Original+string(filepath.Separator))) ||
				(r.Directory && strings.HasPrefix(other.Original, r.Original+string(filepath.Separator))) {
				nested = true
				break
			}
		}
		if !nested {
			parent := filepath.Dir(r.Original)
			groups[parent] = append(groups[parent], r)
		}
	}
	// Each group owns its result slots; independent parents can reach stable
	// storage concurrently without concurrent access to the result map.
	type recoveryGroup struct {
		parent  string
		records []*deletionRecord
		results map[string]error
	}
	work := make([]*recoveryGroup, 0, len(groups))
	for parent, records := range groups {
		work = append(work, &recoveryGroup{parent: parent, records: records})
	}
	_ = deletionParallel(work, func(group *recoveryGroup) error {
		group.results = finishDeletionGroup(group.parent, group.records)
		return nil // Per-record failures are retained in the group's results.
	})
	ret := map[string]error{}
	for _, group := range work {
		maps.Copy(ret, group.results)
	}
	return ret
}

func finishDeletionGroup(parent string, group []*deletionRecord) map[string]error {
	ret := make(map[string]error, len(group))
	var completed []*deletionRecord
	for _, r := range group {
		err := finishIndependentDeletion(r)
		ret[r.ID] = err
		if err == nil {
			completed = append(completed, r)
		}
	}
	if len(completed) == 0 {
		return ret
	}
	var syncErr error
	for _, r := range completed {
		if !directoryIdentityMatches(parent, r.ParentID) {
			syncErr = fmt.Errorf("original parent %q changed during deletion", parent)
			break
		}
	}
	if syncErr == nil {
		syncErr = fsutil.SyncDir(parent)
	}
	if syncErr != nil {
		for _, r := range completed {
			ret[r.ID] = syncErr
		}
	}
	return ret
}

func finishIndependentDeletion(r *deletionRecord) error {
	if !directoryIdentityMatches(filepath.Dir(r.Original), r.ParentID) {
		return fmt.Errorf("original parent of %q is unavailable or has been replaced", r.Original)
	}
	if _, err := os.Lstat(r.StageDir); errors.Is(err, os.ErrNotExist) {
		// A preceding recovery already removed this wrapper. The caller still
		// flushes the parent's absence before removing the journal and marker.
		return nil
	} else if err != nil {
		return err
	}
	if !identityMatches(r.StageDir, r.StageDirID) {
		return fmt.Errorf("staging directory %q has been replaced", r.StageDir)
	}
	if err := removeStaged(r, r.Staged, newDeletionRenamerRemover()); err != nil {
		return err
	}
	// Unexpected wrapper contents keep this operation pending. They never
	// justify recursively deleting a directory reserved for a different file.
	return os.Remove(r.StageDir)
}
