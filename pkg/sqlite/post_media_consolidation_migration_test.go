package sqlite_test

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removePostMediaConsolidationSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeCanonicalPostBackfillSchema(t, raw)
	var exists bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='post_media_consolidation_edges')").Scan(&exists))
	if !exists {
		return
	}
	_, err := raw.Exec(`DROP TRIGGER post_media_supersession_scope;
DROP TRIGGER post_media_decision_scope;
DROP TRIGGER metadata_decision_post_media_scope;
DROP TABLE post_media_consolidation_edges;
DROP INDEX post_media_supersessions_scope;
DELETE FROM native_migration_history WHERE version=1000090;`)
	require.NoError(t, err)
	body, err := os.ReadFile("migrations/1000081_source_post_media_decisions.up.sql")
	require.NoError(t, err)
	for _, name := range []string{"post_media_supersession_scope", "post_media_decision_scope", "metadata_decision_post_media_scope"} {
		_, definition, found := strings.Cut(string(body), "CREATE TRIGGER "+name)
		require.True(t, found)
		definition, _, found = strings.Cut(definition, "\nCREATE TRIGGER ")
		require.True(t, found)
		_, err = raw.Exec("CREATE TRIGGER " + name + definition)
		require.NoError(t, err)
	}
}

func TestPostMediaConsolidationMigrationPreservesExistingChoicesAndRejectsCollision(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserved", true: "collision"}[collision], func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "original"}, "")
			media := archiveFind(t, repo, models.ArchiveScene, 31)
			for _, state := range []string{"linked", "unlinked"} {
				_, err := applyPostMedia(repo, postMediaInput(t, repo, post.UUID, media.UUID, state))
				require.NoError(t, err)
			}
			require.NoError(t, db.Close())
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			removePostMediaConsolidationSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000089,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"source_posts", "source_post_identities", "source_post_consolidations", "post_media_decisions", "post_media_links", "post_media_supersessions", "metadata_field_decisions", "metadata_decision_post_media", "scenes", "archive_entities"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE post_media_consolidation_edges(original TEXT); INSERT INTO post_media_consolidation_edges VALUES('keep unknown data')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(db.DatabasePath()), &needed))
			err = db.RunAllMigrations()
			if collision {
				require.Error(t, err)
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM post_media_consolidation_edges").Scan(&original))
				require.Equal(t, "keep unknown data", original)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='post_media_supersessions_scope'"))
			} else {
				require.NoError(t, err)
				require.NoError(t, db.ReInitialise())
				require.NoError(t, db.Close())
				require.NoError(t, db.Open(db.DatabasePath()))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM post_media_consolidation_edges"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
