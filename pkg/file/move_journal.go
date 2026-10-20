package file

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
)

const (
	moveDirPrefix     = ".stash-move-"
	MaxMoveBatch      = 1000
	maxMoveRecordSize = 64 << 10
)

type moveJournalKey struct{}

// MoveJournal couples filesystem intents to a database transaction. Its commit
// marker must be written in that transaction; Recover runs after the transaction
// ends, under the database writer lock. Records contain no media or audit log.
type MoveJournal struct {
	Directory     string
	MarkCommitted func(string) error
	Recover       func() error

	admitted     map[string]bool
	touched      bool
	once         sync.Once
	err          error
	guardOnce    sync.Once
	guardRecords []*moveRecord
	guardErr     error
}

func WithMoveJournal(ctx context.Context, journal *MoveJournal) context.Context {
	return context.WithValue(ctx, moveJournalKey{}, journal)
}

type moveDirectory struct {
	Path string
	ID   string
}

// gob preserves filenames containing arbitrary bytes, unlike JSON strings.
type moveRecord struct {
	Version           int
	ID                string
	FileID            models.FileID
	Original          string
	Destination       string
	SourceID          string
	SourceParent      moveDirectory
	DestinationParent moveDirectory
	Stage             moveDirectory
	Copy              moveDirectory
	CopyID            string // set only after a complete, flushed copy
	Created           []moveDirectory
	CreatedParent     moveDirectory
}

func (r *moveRecord) staged() string { return filepath.Join(r.Stage.Path, "payload") }
func (r *moveRecord) copied() string { return filepath.Join(r.Copy.Path, "payload") }

func (r *moveRecord) validate() error {
	if _, err := uuid.Parse(r.ID); err != nil || r.Version != 1 || r.FileID <= 0 {
		return errors.New("invalid file move journal version or ID")
	}
	absolute := func(path string) bool { return filepath.IsAbs(path) && filepath.Clean(path) == path }
	if !absolute(r.Original) || !absolute(r.Destination) || r.Original == r.Destination || r.SourceID == "" ||
		r.SourceParent.Path != filepath.Dir(r.Original) || r.DestinationParent.Path != filepath.Dir(r.Destination) ||
		r.SourceParent.ID == "" || r.DestinationParent.ID == "" ||
		filepath.Dir(r.Stage.Path) != r.SourceParent.Path || r.Stage.ID == "" ||
		!strings.HasPrefix(filepath.Base(r.Stage.Path), moveDirPrefix) {
		return errors.New("invalid file move paths or identities")
	}
	if r.Copy.Path != "" && (filepath.Dir(r.Copy.Path) != r.DestinationParent.Path || r.Copy.ID == "" ||
		!strings.HasPrefix(filepath.Base(r.Copy.Path), moveDirPrefix)) {
		return errors.New("invalid file move copy directory")
	}
	if r.CopyID != "" && r.Copy.Path == "" {
		return errors.New("file move copy has no directory")
	}
	for _, dir := range r.Created {
		if !absolute(dir.Path) || dir.ID == "" || !pathWithin(dir.Path, r.DestinationParent.Path) {
			return errors.New("invalid created directory in file move")
		}
	}
	if len(r.Created) > 0 && (r.CreatedParent.Path != filepath.Dir(r.Created[0].Path) || r.CreatedParent.ID == "") {
		return errors.New("invalid parent of created move directories")
	}
	return nil
}

func (j *MoveJournal) prepare(r *moveRecord) error {
	if j.admitted == nil {
		// Unfinished work blocks new moves. It cannot grow by admitting more
		// requests, and no retention policy can discard recovery information.
		if err := RequireNoPendingMoves(j.Directory); err != nil {
			return err
		}
		// Cache the pre-transaction recovery scope before publishing our first
		// intent. Other domain writes in this transaction may read it safely.
		j.loadGuard()
		if j.guardErr != nil {
			return j.guardErr
		}
		j.admitted = make(map[string]bool)
	}
	if len(j.admitted) >= MaxMoveBatch {
		return fmt.Errorf("file move batch exceeds %d files", MaxMoveBatch)
	}
	j.touched = true
	if err := writeMoveRecord(j.Directory, r); err != nil {
		return err
	}
	j.admitted[r.ID] = true
	return j.MarkCommitted(r.ID)
}

// GuardPendingMoves protects only affected file identities and paths. Unrelated
// metadata writes can continue while an external conflict awaits resolution.
// paths is evaluated only if unfinished recovery exists, avoiding extra database
// lookups in normal scans. The enclosing writer lock makes this snapshot stable.
func GuardPendingMoves(ctx context.Context, id models.FileID, paths func() ([]string, error)) error {
	j, _ := ctx.Value(moveJournalKey{}).(*MoveJournal)
	if j == nil {
		return nil
	}
	j.loadGuard()
	if j.guardErr != nil {
		return fmt.Errorf("file move recovery must be repaired before file or folder changes: %w", j.guardErr)
	}
	if len(j.guardRecords) == 0 {
		return nil
	}
	var candidates []string
	if paths != nil {
		var err error
		candidates, err = paths()
		if err != nil {
			return err
		}
	}
	for _, r := range j.guardRecords {
		blocked := id > 0 && id == r.FileID
		for _, path := range candidates {
			if path == "" {
				continue
			}
			if physical, err := filepath.EvalSymlinks(path); err == nil {
				path = physical
			} else if parent, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
				path = filepath.Join(parent, filepath.Base(path))
			}
			if pathWithin(path, r.Original) || pathWithin(path, r.Destination) ||
				pathWithin(r.Original, path) || pathWithin(r.Destination, path) ||
				pathWithin(r.Stage.Path, path) || (r.Copy.Path != "" && pathWithin(r.Copy.Path, path)) {
				blocked = true
			}
		}
		if blocked {
			return fmt.Errorf("file move recovery remains pending for file %d", r.FileID)
		}
	}
	return nil
}

func (j *MoveJournal) loadGuard() {
	j.guardOnce.Do(func() { j.guardRecords, j.guardErr = loadPendingMoves(j.Directory) })
}

func loadPendingMoves(root string) ([]*moveRecord, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("file move journal is not a directory")
	}
	dir, err := os.Open(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(MaxMoveBatch + 2)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > MaxMoveBatch+1 {
		return nil, errors.New("file move recovery directory exceeds its record limit")
	}
	var records []*moveRecord
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".prepare-") && entry.Type().IsRegular() {
			continue
		}
		r, err := readMoveRecord(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

func (j *MoveJournal) complete() error {
	if j.touched {
		j.once.Do(func() { j.err = j.Recover() })
	}
	return j.err
}

// RequireNoPendingMoves must be called under the database writer lock. A native
// checkpoint also uses it: move recovery must finish before copying a library.
func RequireNoPendingMoves(root string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("file move journal is not a directory")
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(1)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("file move recovery remains pending at %q; retry recovery before another move or checkpoint", root)
	}
	return nil
}

func writeMoveRecord(root string, r *moveRecord) error {
	if err := r.validate(); err != nil {
		return err
	}
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(r); err != nil {
		return err
	}
	if buffer.Len() > maxMoveRecordSize {
		return errors.New("file move recovery record exceeds 64 KiB")
	}
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
		return errors.New("file move journal is not a directory")
	}
	f, err := os.CreateTemp(root, ".prepare-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(buffer.Bytes())
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filepath.Join(root, r.ID+".journal")); err != nil {
		return err
	}
	return fsutil.SyncDir(root)
}

func readMoveRecord(path string) (*moveRecord, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxMoveRecordSize {
		return nil, errors.New("invalid file move journal type or size")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r moveRecord
	if err := gob.NewDecoder(io.LimitReader(f, maxMoveRecordSize)).Decode(&r); err != nil {
		return nil, err
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	if filepath.Base(path) != r.ID+".journal" {
		return nil, errors.New("file move journal ID differs from filename")
	}
	return &r, nil
}

// RecoverMoveJournal finishes committed moves and restores aborted ones.
// The caller must hold the database writer lock throughout this call.
func RecoverMoveJournal(root string, committed map[string]bool) (map[string]bool, error) {
	remaining := make(map[string]bool)
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		if err := fsutil.SyncDir(filepath.Dir(root)); err != nil {
			return committed, err
		}
		return remaining, nil
	}
	if err != nil {
		return committed, err
	}
	if !info.IsDir() {
		return committed, errors.New("file move journal is not a directory")
	}
	dir, err := os.Open(root)
	if err != nil {
		return committed, err
	}
	defer dir.Close()
	// A process can leave one interrupted rewrite in addition to its intents.
	entries, err := dir.ReadDir(MaxMoveBatch + 2)
	if err != nil && !errors.Is(err, io.EOF) {
		return committed, err
	}
	if len(entries) > MaxMoveBatch+1 {
		return committed, errors.New("file move recovery directory exceeds its record limit")
	}
	var errs []error
	var completed []string
	unreadable := false
	var recovered []*moveRecord
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(root, name)
		if strings.HasPrefix(name, ".prepare-") && entry.Type().IsRegular() {
			// No effect may depend on an unpublished intent or update.
			errs = append(errs, os.Remove(path))
			continue
		}
		id := strings.TrimSuffix(name, ".journal")
		remaining[id] = true
		r, err := readMoveRecord(path)
		if err == nil {
			err = completeMove(r, committed[id])
		} else {
			unreadable = true
		}
		if err == nil {
			recovered = append(recovered, r)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("recovering file move %q: %w", name, err))
		}
	}
	// Restore every file before removing any shared directories. Otherwise an
	// earlier record would fail cleanup simply because a later file is staged.
	for _, r := range recovered {
		var err error
		if !committed[r.ID] {
			err = removeCreatedMoveDirectories(r)
		}
		if err == nil {
			err = os.Remove(filepath.Join(root, r.ID+".journal"))
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("cleaning file move %q: %w", r.ID, err))
		} else {
			completed = append(completed, r.ID)
		}
	}
	if err := fsutil.SyncDir(root); err != nil {
		return remaining, errors.Join(append(errs, err)...)
	}
	for _, id := range completed {
		delete(remaining, id)
	}
	if unreadable {
		// A damaged filename or record may hide which marker belongs to it.
		// Keep all outcomes until the record can be interpreted safely.
		for id := range committed {
			remaining[id] = true
		}
	}
	if len(remaining) == 0 {
		if err := os.Remove(root); err != nil {
			errs = append(errs, err)
		} else {
			errs = append(errs, fsutil.SyncDir(filepath.Dir(root)))
		}
	}
	return remaining, errors.Join(errs...)
}
