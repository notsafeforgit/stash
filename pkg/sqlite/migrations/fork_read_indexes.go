package migrations

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/sqlite"
)

// Ordinary covering indexes are maintained by upstream SQLite writes too. No
// fork columns, triggers, virtual tables, or custom collations are required.
// The title payload lets SQLite sort equal timestamps without fetching rows;
// the existing query still controls natural title ordering and ID tie-breaks.
func reconcileReadIndexes(ctx context.Context, db *sqlx.DB) error {
	for _, table := range []string{"scenes", "images"} {
		query := fmt.Sprintf("CREATE INDEX IF NOT EXISTS fork_%s_created_at ON %s (created_at, title)", table, table)
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("creating %s browsing index: %w", table, err)
		}
	}
	return nil
}

func init() {
	sqlite.RegisterForkMigration(6, "read performance indexes", reconcileReadIndexes)
	// An upstream table rebuild may discard an optional index. Recreate it when
	// returning to the fork, without changing upstream's schema version.
	sqlite.RegisterForkReconciler("read performance indexes", reconcileReadIndexes)
}
