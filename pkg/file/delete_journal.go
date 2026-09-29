package file

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/fsutil"
)

type deletionJournalKey struct{}

// DeletionJournal connects filesystem intents to the enclosing database
// transaction. MarkCommitted must write inside that transaction. Recover must
// run after it ends, holding the database writer lock throughout recovery.
type DeletionJournal struct {
	Directory     string
	MarkCommitted func(id string) error
	Recover       func() error
	once          sync.Once
	err           error
	touched       bool
}

func WithDeletionJournal(ctx context.Context, journal *DeletionJournal) context.Context {
	return context.WithValue(ctx, deletionJournalKey{}, journal)
}

func (j *DeletionJournal) prepare(r *deletionRecord) error {
	j.touched = true
	if err := writeDeletionRecord(j.Directory, r); err != nil {
		return err
	}
	return j.MarkCommitted(r.ID)
}

func (j *DeletionJournal) complete() error {
	if !j.touched {
		return nil
	}
	j.once.Do(func() { j.err = j.Recover() })
	return j.err
}

// Paths are encoded with gob, preserving arbitrary filesystem bytes rather
// than replacing invalid UTF-8 as JSON strings would. This is a pending-work
// record, never an audit log: it is removed after durable completion.
type deletionRecord struct {
	Version     int
	ID          string
	Original    string
	Staged      string
	StageDir    string
	SourceID    string
	ParentID    string
	StageDirID  string
	Directory   bool
	TrashRoot   string
	TrashRootID string
	Destination string
	TrashDirID  string
	CopyDirID   string
	CopyID      string
}

func writeDeletionRecord(root string, r *deletionRecord) error {
	if err := os.Mkdir(root, 0700); err == nil {
		if err := fsutil.SyncDir(filepath.Dir(root)); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("deletion journal %q is not a directory", root)
	}
	f, err := os.CreateTemp(root, ".prepare-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	err = gob.NewEncoder(f).Encode(r)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(root, r.ID+".journal")); err != nil {
		return err
	}
	return fsutil.SyncDir(root)
}

func readDeletionRecord(path string) (*deletionRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r deletionRecord
	if err := gob.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&r); err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	if filepath.Base(path) != r.ID+".journal" {
		return nil, errors.New("deletion journal ID does not match its filename")
	}
	return &r, nil
}

func (r *deletionRecord) validate() error {
	if _, err := uuid.Parse(r.ID); err != nil || r.Version != 1 {
		return errors.New("invalid deletion journal version or ID")
	}
	if !filepath.IsAbs(r.Original) || filepath.Clean(r.Original) != r.Original ||
		filepath.Dir(r.StageDir) != filepath.Dir(r.Original) ||
		!strings.HasPrefix(filepath.Base(r.StageDir), deleteDirPrefix) ||
		r.Staged != filepath.Join(r.StageDir, filepath.Base(r.Original)) ||
		r.SourceID == "" || r.ParentID == "" || r.StageDirID == "" {
		return errors.New("invalid deletion journal paths or file identities")
	}
	if r.TrashRoot != "" && (!filepath.IsAbs(r.TrashRoot) || filepath.Clean(r.TrashRoot) != r.TrashRoot) {
		return errors.New("invalid trash root in deletion journal")
	}
	if r.Destination != "" && (r.TrashRoot == "" ||
		filepath.Dir(filepath.Dir(r.Destination)) != r.TrashRoot ||
		!strings.HasPrefix(filepath.Base(filepath.Dir(r.Destination)), "stash-trash-") ||
		filepath.Base(r.Destination) != filepath.Base(r.Original) || r.TrashDirID == "") {
		return errors.New("invalid trash destination in deletion journal")
	}
	return nil
}

// RecoverDeletionJournal reconciles intents against durable commit markers.
// The caller MUST hold the database writer lock so no live transaction can be
// mistaken for an aborted one. Remaining IDs retain their database markers.
func RecoverDeletionJournal(root string, committed map[string]bool) (map[string]bool, error) {
	remaining := make(map[string]bool)
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		// Persist the absence before allowing orphan commit markers to go away.
		if err := fsutil.SyncDir(filepath.Dir(root)); err != nil {
			return committed, err
		}
		return remaining, nil
	}
	if err != nil {
		return committed, err
	}
	if !info.IsDir() {
		return committed, fmt.Errorf("deletion journal %q is not a directory", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return committed, err
	}
	var records []*deletionRecord
	var errs []error
	unknownPaths := false
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".prepare-") && !entry.IsDir() {
			// No move may start before the finished intent is published and synced.
			if err := os.Remove(filepath.Join(root, name)); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if !strings.HasSuffix(name, ".journal") {
			continue
		}
		id := strings.TrimSuffix(name, ".journal")
		remaining[id] = true
		if !entry.Type().IsRegular() {
			unknownPaths = true
			errs = append(errs, fmt.Errorf("deletion journal %q is not a regular file", name))
			continue
		}
		r, err := readDeletionRecord(filepath.Join(root, name))
		if err != nil {
			unknownPaths = true
			errs = append(errs, fmt.Errorf("reading deletion journal %q: %w", name, err))
			continue
		}
		records = append(records, r)
	}

	// Restore parents first, then finalize committed children before parents.
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if committed[a.ID] != committed[b.ID] {
			return !committed[a.ID]
		}
		if committed[a.ID] {
			return len(a.Original) > len(b.Original)
		}
		return len(a.Original) < len(b.Original)
	})
	for _, r := range records {
		if committed[r.ID] && r.Directory && unknownPaths {
			// A damaged intent may describe a staged child inside this directory.
			errs = append(errs, fmt.Errorf("deletion of %q is waiting for unreadable recovery records", r.Original))
			continue
		}
		if committed[r.ID] && hasPendingChild(r, records, remaining) {
			errs = append(errs, fmt.Errorf("deletion of %q is waiting for a child operation", r.Original))
			continue
		}
		err := completeDeletion(r, records, committed[r.ID], newDeletionRenamerRemover(), func() error {
			return writeDeletionRecord(root, r)
		})
		if err == nil {
			err = os.Remove(filepath.Join(root, r.ID+".journal"))
			if err == nil {
				err = fsutil.SyncDir(root)
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("recovering deletion of %q (staged at %q): %w", r.Original, r.Staged, err))
			continue
		}
		delete(remaining, r.ID)
	}
	if err := fsutil.SyncDir(root); err != nil {
		errs = append(errs, err)
	}
	if entries, err := os.ReadDir(root); err == nil && len(entries) == 0 {
		if err := os.Remove(root); err != nil {
			errs = append(errs, err)
		} else if err := fsutil.SyncDir(filepath.Dir(root)); err != nil {
			errs = append(errs, err)
		}
	}
	return remaining, errors.Join(errs...)
}

func hasPendingChild(r *deletionRecord, records []*deletionRecord, remaining map[string]bool) bool {
	if !r.Directory {
		return false
	}
	for _, other := range records {
		if remaining[other.ID] && strings.HasPrefix(other.Original, r.Original+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func identityMatches(path, expected string) bool {
	id, err := fsutil.FileIdentity(path)
	return err == nil && id == expected
}

func directoryIdentityMatches(path, expected string) bool {
	id, err := fsutil.DirectoryIdentity(path)
	return err == nil && id == expected
}

// A parent directory may itself have been staged. Resolve nested operations
// through that parent's recorded location rather than touching a replacement.
func deletionLocation(path string, records []*deletionRecord) string {
	var closest *deletionRecord
	for _, r := range records {
		if r.Directory && (path == r.Original || strings.HasPrefix(path, r.Original+string(filepath.Separator))) &&
			(closest == nil || len(r.Original) > len(closest.Original)) {
			closest = r
		}
	}
	if closest == nil || identityMatches(closest.Original, closest.SourceID) {
		return path
	}
	base := deletionLocation(closest.Staged, records)
	if !identityMatches(base, closest.SourceID) {
		return path
	}
	rel, _ := filepath.Rel(closest.Original, path)
	return filepath.Join(base, rel)
}
