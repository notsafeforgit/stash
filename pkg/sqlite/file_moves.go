package sqlite

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
)

// FileMoveJournalPath identifies this library's bounded, pending recovery work.
func (db *Database) FileMoveJournalPath() string { return db.fileJournalPath("moves") }

func guardFileMove(ctx context.Context, base *models.BaseFile) error {
	return file.GuardPendingMoves(ctx, base.ID, func() ([]string, error) {
		var parent string
		if err := dbWrapper.Get(ctx, &parent, "SELECT path FROM folders WHERE id=?", base.ParentFolderID); err != nil {
			return nil, err
		}
		return []string{base.Path, filepath.Join(parent, base.Basename)}, nil
	})
}

func guardFileMoveIdentity(ctx context.Context, id models.FileID) error {
	return file.GuardPendingMoves(ctx, id, nil)
}

func guardFolderMove(ctx context.Context, id models.FolderID, path string) error {
	return file.GuardPendingMoves(ctx, 0, func() ([]string, error) {
		if id == 0 {
			return []string{path}, nil
		}
		var previous string
		if err := dbWrapper.Get(ctx, &previous, "SELECT path FROM folders WHERE id=?", id); err != nil {
			return nil, err
		}
		return []string{previous, path}, nil
	})
}

func guardFolderMoveUpdate(ctx context.Context, updated *models.Folder) error {
	return file.GuardPendingMoves(ctx, 0, func() ([]string, error) {
		previous, err := NewFolderStore().Find(ctx, updated.ID)
		if err != nil {
			return nil, err
		}
		equalID := func(a, b *models.FileID) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
		equalParent := func(a, b *models.FolderID) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
		if previous.Path == updated.Path && equalID(previous.ZipFileID, updated.ZipFileID) &&
			equalParent(previous.ParentFolderID, updated.ParentFolderID) {
			return nil, nil // Updating scan timestamps does not change the path.
		}
		return []string{previous.Path, updated.Path}, nil
	})
}

func (db *Database) withFileMoveJournal(ctx context.Context, tx *sqlx.Tx, writable bool) context.Context {
	if !writable || db.Version() < NativeSchemaBaseline+124 {
		return ctx
	}
	batchStarted := false
	return file.WithMoveJournal(ctx, &file.MoveJournal{
		Directory: db.FileMoveJournalPath(),
		MarkCommitted: func(id string) error {
			if !batchStarted {
				var pending bool
				if err := tx.GetContext(ctx, &pending, "SELECT EXISTS(SELECT 1 FROM file_moves)"); err != nil {
					return err
				}
				if pending {
					return errors.New("file move recovery is required before admitting another batch")
				}
			}
			_, err := tx.ExecContext(ctx, "INSERT INTO file_moves(id) VALUES(?)", id)
			batchStarted = err == nil
			return err
		},
		Recover: db.RecoverFileMoves,
	})
}

// RecoverFileMoves excludes all active writers until intents have been resolved
// against committed markers. Failed recovery keeps its record and marker.
func (db *Database) RecoverFileMoves() error {
	if db.Version() < NativeSchemaBaseline+124 {
		return nil
	}
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
	if err := tx.SelectContext(ctx, &ids, "SELECT id FROM file_moves"); err != nil {
		return err
	}
	committed := make(map[string]bool, len(ids))
	for _, id := range ids {
		committed[id] = true
	}
	remaining, recoveryErr := file.RecoverMoveJournal(db.FileMoveJournalPath(), committed)
	for _, id := range ids {
		if !remaining[id] {
			if _, err := tx.ExecContext(ctx, "DELETE FROM file_moves WHERE id = ?", id); err != nil {
				return errors.Join(recoveryErr, err)
			}
		}
	}
	err = db.Commit(ctx)
	finished = err == nil
	return errors.Join(recoveryErr, err)
}
