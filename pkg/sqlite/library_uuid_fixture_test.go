package sqlite_test

import (
	"database/sql"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
)

// The migration fixture chain strips later schemas before replaying an older
// migration. Restore the actual eight-kind table, not just its version number.
func removeLibraryUUIDSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	removeContinuousSourceSchema(t, db)
	if queryUint(t, db, "SELECT count(*) FROM pragma_table_info('archive_entities') WHERE name='folder_id'") == 0 {
		return
	}
	// Match the migrator's dedicated connection, which disables FK enforcement
	// while rebuilding a referenced table and restores it before ordinary use.
	db.SetMaxOpenConns(1)
	_, fkErr := db.Exec("PRAGMA foreign_keys=OFF")
	require.NoError(t, fkErr)
	defer func() { _, err := db.Exec("PRAGMA foreign_keys=ON"); require.NoError(t, err) }()
	var definitions []string
	func() {
		rows, err := db.Query(`SELECT sql FROM sqlite_schema WHERE tbl_name='archive_entities' AND sql IS NOT NULL AND type IN ('trigger','index') AND name NOT IN ('archive_entities_folder','archive_entities_scene_marker','archive_entities_gallery_chapter','archive_entities_saved_filter')`)
		require.NoError(t, err)
		for rows.Next() {
			var statement string
			require.NoError(t, rows.Scan(&statement))
			definitions = append(definitions, statement)
		}
		require.NoError(t, rows.Err())
		defer rows.Close()
	}()

	for _, kind := range []string{"folder", "scene_marker", "gallery_chapter", "saved_filter"} {
		for _, action := range []string{"created", "changed", "deleted"} {
			_, err := db.Exec("DROP TRIGGER archive_" + kind + "_" + action)
			require.NoError(t, err)
		}
	}
	_, err := db.Exec(`CREATE TEMP TABLE old_library_identities AS SELECT uuid,kind,state,revision,performer_id,scene_id,image_id,file_id,gallery_id,tag_id,studio_id,group_id,original_id,redirect_to,created_at,retired_at FROM archive_entities WHERE kind NOT IN ('folder','scene_marker','gallery_chapter','saved_filter'); DROP TABLE archive_entities;`)
	require.NoError(t, err)
	body, err := os.ReadFile("migrations/1000011_metadata_identities.up.sql")
	require.NoError(t, err)
	definition := string(body)
	start := strings.Index(definition, "CREATE TABLE archive_entities (")
	require.NotEqual(t, -1, start)
	definition = definition[start:]
	end := strings.Index(definition, "\n);\n")
	require.NotEqual(t, -1, end)
	definition = definition[:end+4]
	_, err = db.Exec(definition)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO archive_entities(uuid,kind,state,revision,performer_id,scene_id,image_id,file_id,gallery_id,tag_id,studio_id,group_id,original_id,redirect_to,created_at,retired_at) SELECT * FROM old_library_identities; DROP TABLE old_library_identities; ALTER TABLE scene_cover_sources DROP COLUMN source_file_uuid; DELETE FROM native_migration_history WHERE version=1000115;`)
	require.NoError(t, err)
	for _, statement := range definitions {
		_, err = db.Exec(statement)
		require.NoError(t, err)
	}
}
