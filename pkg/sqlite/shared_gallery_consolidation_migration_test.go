package sqlite_test

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeConsolidatedSharedGallerySchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	prior, err := os.ReadFile("migrations/1000117_overlapping_source_galleries.up.sql")
	require.NoError(t, err)
	definition := strings.Split(strings.Split(string(prior), "DROP TRIGGER post_gallery_thread_share_insert;")[1], "DROP TRIGGER post_gallery_thread_share_update;")[0]
	_, err = raw.Exec("DROP TRIGGER post_gallery_thread_share_insert;" + definition + ";DELETE FROM native_migration_history WHERE version=1000139;")
	require.NoError(t, err)
}

func TestSharedGalleryConsolidationMigrationPreservesExistingMediaAndMetadata(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	singleSourceAlbum(t, repo)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeConsolidatedSharedGallerySchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000138,dirty=0")
	require.NoError(t, err)
	before := map[string][][]any{}
	for _, table := range []string{"galleries", "scenes", "images", "post_gallery_links", "post_gallery_decisions", "metadata_field_decisions"} {
		before[table] = albumJobRows(t, raw, table)
	}
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, db.Open(path), &needed)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.Open(path))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_post_consolidation_context"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
