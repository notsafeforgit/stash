package sqlite

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// This version is deliberately pinned independently of appSchemaVersion.
// Numbered native migrations after it still run through the normal migrator.
const nativeBootstrapVersion uint = 1000121

//go:embed bootstrap/1000121.sql
var nativeBootstrapSQL string

// A missing/zero-length file or an untouched SQLite database is fresh. An empty
// migration ledger, views without tables, or the header of a previously used
// database are not evidence of freshness.
func isPristineDatabase(ctx context.Context, conn sqlx.QueryerContext) (bool, error) {
	var exists bool
	if err := sqlx.GetContext(ctx, conn, &exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema)"); err != nil || exists {
		return false, err
	}
	for _, pragma := range []string{"schema_version", "user_version", "application_id"} {
		var value int
		if err := sqlx.GetContext(ctx, conn, &value, "PRAGMA "+pragma); err != nil {
			return false, err
		}
		if value != 0 {
			return false, nil
		}
	}
	return true, nil
}

func (m *Migrator) bootstrapNative(ctx context.Context) (bool, error) {
	fresh, err := isPristineDatabase(ctx, m.conn)
	if err != nil || !fresh {
		return false, err
	}
	// The writable connection uses BEGIN IMMEDIATE and synchronous=FULL. Recheck
	// under its writer lock before creating anything, including the migration
	// driver's ledger. A failure or process interruption rolls everything back.
	tx, err := m.conn.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	fresh, err = isPristineDatabase(ctx, tx)
	if err != nil {
		return false, err
	}
	if !fresh {
		return false, fmt.Errorf("database changed during native initialization; reopen to validate its schema")
	}
	if _, err := tx.ExecContext(ctx, nativeBootstrapSQL); err != nil {
		return false, fmt.Errorf("initializing native schema %d: %w", nativeBootstrapVersion, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("committing native initialization: %w", err)
	}
	return true, nil
}
