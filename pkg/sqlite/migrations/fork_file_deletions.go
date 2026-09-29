package migrations

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/sqlite"
)

func migrateFileDeletions(ctx context.Context, db *sqlx.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS fork_file_deletions (
		id TEXT PRIMARY KEY NOT NULL
	)`)
	return err
}

func init() {
	sqlite.RegisterForkMigration(9, "recoverable file deletions", migrateFileDeletions)
	sqlite.RegisterForkReconciler("recoverable file deletions", migrateFileDeletions)
}
