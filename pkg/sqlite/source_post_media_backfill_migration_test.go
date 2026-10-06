package sqlite_test

import (
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removePostMediaBackfillSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP TABLE post_media_decision_evidence; DROP TABLE post_media_backfill_decisions; DROP TABLE post_media_backfills;
DELETE FROM native_migration_history WHERE version=1000082;`)
	require.NoError(t, err)
}

func TestPostMediaBackfillMigrationRetainsEvidenceAndExistingChoices(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "migration", true: "collision"}[collision], func(t *testing.T) {
			f, post, evidence := postBackfillFixture(t)
			_, err := applyPostMedia(f.repo, postMediaInput(t, f.repo, post, evidence.MediaUUID, "unlinked"))
			require.NoError(t, err)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			removePostMediaBackfillSchema(t, raw)
			_, err = raw.Exec("UPDATE schema_migrations SET version=1000081,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"post_media_decisions", "post_media_links", "source_posts", "source_media_evidence", "source_post_file_evidence", "source_file_matches", "scenes", "files"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE post_media_backfills(original TEXT); INSERT INTO post_media_backfills VALUES('retained unknown record')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
			err = f.db.RunAllMigrations()
			if collision {
				require.Error(t, err)
				var value string
				require.NoError(t, raw.QueryRow("SELECT original FROM post_media_backfills").Scan(&value))
				require.Equal(t, "retained unknown record", value)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='post_media_decision_evidence'"))
			} else {
				require.NoError(t, err)
				require.NoError(t, f.db.ReInitialise())
				require.NoError(t, f.db.Close())
				require.NoError(t, f.db.Open(f.db.DatabasePath()))
				require.Equal(t, "unlinked", postMediaAssociation(t, f.db.Repository(), post, evidence.MediaUUID).State)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM post_media_backfills"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
		})
	}
}

func TestPostMediaBackfillStartupRejectsIncompleteReceiptBeforeWriting(t *testing.T) {
	f, post, _ := postBackfillFixture(t)
	result, err := applyPostBackfill(f.repo, postBackfillRequest(previewPostBackfill(t, f.repo, post)))
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var trigger string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='post_media_backfill_immutable'").Scan(&trigger))
	_, err = raw.Exec("DROP TRIGGER post_media_backfill_immutable")
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE post_media_backfills SET selected=2 WHERE uuid=?", result.UUID)
	require.NoError(t, err)
	_, err = raw.Exec(trigger)
	require.NoError(t, err)
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, f.db.Open(f.db.DatabasePath()), "invalid historical post media backfills")
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}
