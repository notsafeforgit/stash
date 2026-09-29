package sqlite

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/file"
)

// FileDeletionJournalPath is a bounded-length sibling directory, created only
// when needed. Completed operations and the empty directory are removed.
func (db *Database) FileDeletionJournalPath() string {
	path, _ := filepath.Abs(db.dbPath)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	// Resolve aliases of the database and keep the name stable if its containing
	// directory moves. Different databases in one directory still stay separate.
	id := sha256.Sum256([]byte(filepath.Base(path)))
	return filepath.Join(filepath.Dir(path), fmt.Sprintf(".stash-deletions-%x", id[:8]))
}

func (db *Database) withFileDeletionJournal(ctx context.Context, tx *sqlx.Tx, writable bool) context.Context {
	if !writable {
		return ctx
	}
	return file.WithDeletionJournal(ctx, &file.DeletionJournal{
		Directory: db.FileDeletionJournalPath(),
		MarkCommitted: func(id string) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO fork_file_deletions (id) VALUES (?)", id)
			return err
		},
		Recover: db.RecoverFileDeletions,
	})
}

// RecoverFileDeletions retries unfinished operations. BEGIN IMMEDIATE excludes
// active writers (including other Stash processes) until recovery finishes.
// Neither a transaction still staging files nor another recovery can race it.
func (db *Database) RecoverFileDeletions() error {
	ctx, err := db.Begin(context.Background(), true)
	if err != nil {
		return err
	}
	finished := false
	defer func() {
		if !finished {
			_ = db.Rollback(ctx)
		}
	}()
	tx, err := getTx(ctx)
	if err != nil {
		return err
	}
	var ids []string
	if err := tx.SelectContext(ctx, &ids, "SELECT id FROM fork_file_deletions"); err != nil {
		return err
	}
	committed := make(map[string]bool, len(ids))
	for _, id := range ids {
		committed[id] = true
	}
	remaining, recoveryErr := file.RecoverDeletionJournal(db.FileDeletionJournalPath(), committed)
	for _, id := range ids {
		if remaining[id] {
			continue
		}
		// The intent was durably removed first. A crash before this DELETE
		// commits leaves only an orphan marker, safe to prune on the next run.
		if _, err := tx.ExecContext(ctx, "DELETE FROM fork_file_deletions WHERE id = ?", id); err != nil {
			return errors.Join(recoveryErr, err)
		}
	}
	err = db.Commit(ctx)
	finished = err == nil
	return errors.Join(recoveryErr, err)
}
