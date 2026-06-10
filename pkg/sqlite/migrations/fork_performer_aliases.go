package migrations

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/sqlite"
)

func fork001PerformerAliasesIgnoreAutoTag(ctx context.Context, db *sqlx.DB) error {
	exists, err := forkColumnExists(ctx, db, "performer_aliases", "ignore_auto_tag")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	if _, err := db.ExecContext(ctx, "ALTER TABLE `performer_aliases` ADD COLUMN `ignore_auto_tag` BOOLEAN DEFAULT 1 NOT NULL"); err != nil {
		return fmt.Errorf("adding performer_aliases.ignore_auto_tag: %w", err)
	}

	return nil
}

func init() {
	sqlite.RegisterLegacyForkMigration(1, "performer aliases ignore_auto_tag", 998, fork001PerformerAliasesIgnoreAutoTag)
}
