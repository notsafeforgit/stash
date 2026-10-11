package file

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/stashapp/stash/pkg/fsutil"
)

type queuedDeletion struct {
	path, sourceID, parentID string
	directory, bypassTrash   bool
}

func (d *Deleter) queue(path string, directory, bypassTrash bool) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	absolute = filepath.Join(parent, filepath.Base(absolute))
	for _, queued := range d.queued {
		if queued.path == absolute {
			if queued.directory != directory || queued.bypassTrash != bypassTrash {
				return errors.New("conflicting options for a queued deletion")
			}
			return nil
		}
	}
	sourceID, err := fsutil.FileIdentity(absolute)
	if err != nil {
		return err
	}
	parentID, err := fsutil.DirectoryIdentity(parent)
	if err != nil {
		return err
	}
	d.queued = append(d.queued, queuedDeletion{absolute, sourceID, parentID, directory, bypassTrash})
	return nil
}

// Flush stages the queued paths before database commit. Every recovery intent
// is durable before the first rename; every rename is durable before returning.
// It is also the transaction's pre-commit guard, so a failed flush rolls back.
func (d *Deleter) Flush(ctx context.Context) error {
	if d.stagingErr != nil {
		return d.stagingErr
	}
	d.stagingErr = d.flushBatch(ctx)
	return d.stagingErr
}

func (d *Deleter) flushBatch(ctx context.Context) error {
	if len(d.queued) == 0 {
		return nil
	}
	queued := d.queued
	d.queued = nil
	// Nested deletions use the established child-before-parent protocol. Bulk
	// scenes/images normally have independent originals and preview directories.
	slices.SortFunc(queued, func(a, b queuedDeletion) int { return len(b.path) - len(a.path) })
	nested := false
	for i, item := range queued {
		for _, parent := range queued[i+1:] {
			if strings.HasPrefix(item.path, parent.path+string(filepath.Separator)) {
				nested = true
			}
		}
		for _, prior := range d.pending {
			if strings.HasPrefix(item.path, prior.Original+string(filepath.Separator)) || strings.HasPrefix(prior.Original, item.path+string(filepath.Separator)) {
				nested = true
			}
		}
	}
	var records []*deletionRecord
	movesStarted := false
	defer func() {
		if !movesStarted {
			for _, record := range records {
				// These reservations are still empty. Never remove a payload.
				_ = d.RenamerRemover.Remove(record.StageDir)
			}
		}
	}()
	for _, item := range queued {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !identityMatches(item.path, item.sourceID) || !directoryIdentityMatches(filepath.Dir(item.path), item.parentID) {
			return fmt.Errorf("queued deletion path %q was replaced", item.path)
		}
		if nested {
			if err := d.stage(item.path, item.directory, item.bypassTrash); err != nil {
				return err
			}
			continue
		}
		r, err := d.plan(item.path, item.directory, item.bypassTrash)
		if err != nil {
			return err
		}
		records = append(records, r)
		if r.SourceID != item.sourceID || r.ParentID != item.parentID {
			return fmt.Errorf("queued deletion path %q changed while preparing", item.path)
		}
	}
	if nested {
		return nil
	}
	if err := d.journal.prepareBatch(records); err != nil {
		return err
	}
	d.pending = append(d.pending, records...)
	movesStarted = true
	parents := map[string]bool{}
	for _, r := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !identityMatches(r.Original, r.SourceID) || !directoryIdentityMatches(filepath.Dir(r.Original), r.ParentID) {
			return fmt.Errorf("queued deletion path %q was replaced before staging", r.Original)
		}
		if err := d.RenamerRemover.Rename(r.Original, r.Staged); err != nil {
			return err
		}
		if !identityMatches(r.Staged, r.SourceID) {
			return errors.New("source was replaced during batch deletion staging")
		}
		parents[filepath.Dir(r.Original)] = true
	}
	// Independent flushes share a bounded worker pool. Common source parents
	// are flushed once after every staging directory has reached stable storage.
	if err := deletionParallel(records, func(r *deletionRecord) error { return fsutil.SyncDir(r.StageDir) }); err != nil {
		return err
	}
	if err := deletionParallel(slices.Collect(maps.Keys(parents)), fsutil.SyncDir); err != nil {
		return err
	}
	return ctx.Err()
}

func (j *DeletionJournal) prepareBatch(records []*deletionRecord) error {
	j.touched = true
	if err := prepareDeletionJournalDirectory(j.Directory); err != nil {
		return err
	}
	if err := deletionParallel(records, func(r *deletionRecord) error { return writeDeletionRecordFile(j.Directory, r) }); err != nil {
		return err
	}
	if err := fsutil.SyncDir(j.Directory); err != nil {
		return err
	}
	// The enclosing SQLite transaction has one writer. Only filesystem work
	// runs concurrently; its commit markers stay on that transaction's goroutine.
	for _, r := range records {
		if err := j.MarkCommitted(r.ID); err != nil {
			return err
		}
	}
	return nil
}

func deletionParallel[T any](items []T, fn func(T) error) error {
	var wg sync.WaitGroup
	work := make(chan int)
	errs := make([]error, len(items))
	for range min(8, len(items)) {
		wg.Go(func() {
			for i := range work {
				errs[i] = fn(items[i])
			}
		})
	}
	for i := range items {
		work <- i
	}
	close(work)
	wg.Wait()
	return errors.Join(errs...)
}
