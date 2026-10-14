package sqlite_test

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeGalleryConsolidationSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	if queryUint(t, db, "SELECT count(*) FROM sqlite_schema WHERE name='gallery_alternate_titles'") == 0 {
		return
	}
	body, err := os.ReadFile("migrations/1000105_source_threads.up.sql")
	require.NoError(t, err)
	original := string(body)
	original = original[strings.Index(original, "CREATE TRIGGER post_gallery_thread_share_insert"):strings.Index(original, "INSERT INTO native_migration_history")]
	_, err = db.Exec(`DROP TABLE gallery_alternate_titles; DROP TABLE source_gallery_merge_context;
DROP TRIGGER post_gallery_thread_share_insert; DROP TRIGGER post_gallery_thread_share_update;
DELETE FROM native_migration_history WHERE version=1000117;` + original)
	require.NoError(t, err)
}

func TestGalleryConsolidationMigrationDoesNotMergeUntilRequested(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	a := consolidationGallery(t, repo, "One", []int{41}, nil)
	b := consolidationGallery(t, repo, "Two", []int{41}, nil)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeGalleryConsolidationSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000116")
	require.NoError(t, err)
	before := albumJobRows(t, raw, "galleries_images")
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	require.Equal(t, before, albumJobRows(t, raw, "galleries_images"))
	repo = db.Repository()
	p := consolidationPreview(t, repo, a.UUID)
	require.Len(t, p.Galleries, 2)
	require.ElementsMatch(t, []string{a.UUID, b.UUID}, []string{p.Galleries[0].UUID, p.Galleries[1].UUID})
	consolidationApply(t, repo, a.UUID, p)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
