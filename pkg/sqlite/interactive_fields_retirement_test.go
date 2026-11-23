package sqlite_test

import (
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// Schema 1000145 drops the funscript interactive columns from video_files and
// keeps every video file.
func TestInteractiveFieldsRetirementKeepsVideoFiles(t *testing.T) {
	db, _, id := newDeletionDatabase(t)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeInteractiveFieldsSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000144,dirty=0")
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE video_files SET interactive=1,interactive_speed=42 WHERE file_id=?", id)
	require.NoError(t, err)
	before := albumJobRows(t, raw, "files")

	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, db.Open(path), &needed)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.Open(path))
	require.NoError(t, db.AuditForTesting(path))

	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_table_info('video_files') WHERE name IN ('interactive','interactive_speed')"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM video_files WHERE file_id="+id.String()))
	require.Equal(t, before, albumJobRows(t, raw, "files"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	var details string
	require.NoError(t, raw.QueryRow("SELECT details FROM native_migration_history WHERE version=1000145").Scan(&details))
	require.JSONEq(t, `{"interactive_files":1,"files_with_speed":1}`, details)
}
