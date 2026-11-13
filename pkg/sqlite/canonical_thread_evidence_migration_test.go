package sqlite_test

import (
	"database/sql"
	"os"
	"regexp"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeCanonicalThreadEvidenceSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeMetadataCurrentValuesSchema(t, raw)
	for name, file := range map[string]string{
		"source_post_thread_scope":         "migrations/1000105_source_threads.up.sql",
		"post_gallery_thread_share_insert": "migrations/1000139_consolidated_shared_galleries.up.sql",
		"post_gallery_thread_share_update": "migrations/1000117_overlapping_source_galleries.up.sql",
	} {
		prior, err := os.ReadFile(file)
		require.NoError(t, err)
		definition := regexp.MustCompile(`(?s)CREATE TRIGGER ` + name + `\b.*?END;`).FindString(string(prior))
		require.NotEmpty(t, definition)
		_, err = raw.Exec("DROP TRIGGER " + name + ";" + definition)
		require.NoError(t, err)
	}
	_, err := raw.Exec("DELETE FROM native_migration_history WHERE version=1000140")
	require.NoError(t, err)
}

func TestCanonicalThreadEvidenceMigrationPreservesStoredEvidenceAndMedia(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	singleSourceAlbum(t, repo)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeCanonicalThreadEvidenceSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000139,dirty=0")
	require.NoError(t, err)
	before := map[string][][]any{}
	for _, table := range []string{"source_posts", "source_post_identities", "source_post_threads", "source_captures", "galleries", "images", "scenes", "post_gallery_links", "post_gallery_decisions", "metadata_field_decisions"} {
		before[table] = albumJobRows(t, raw, table)
	}
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, db.Open(path), &needed)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.Open(path))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
