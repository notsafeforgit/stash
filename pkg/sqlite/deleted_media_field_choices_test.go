package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func archiveIdentity(t *testing.T, repo models.Repository, id string) *models.ArchiveEntity {
	t.Helper()
	var ret *models.ArchiveEntity
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.ArchiveEntity.Find(ctx, id)
		return err
	}))
	require.NotNil(t, ret)
	return ret
}

func fieldRecordCount(t *testing.T, raw *sql.DB, entity string) uint {
	t.Helper()
	return queryUint(t, raw, fmt.Sprintf(`SELECT (SELECT count(*) FROM metadata_field_decisions WHERE entity_uuid='%[1]s')
 + (SELECT count(*) FROM metadata_field_heads WHERE entity_uuid='%[1]s')
 + (SELECT count(*) FROM metadata_field_baselines WHERE entity_uuid='%[1]s')
 + (SELECT count(*) FROM metadata_field_pending WHERE entity_uuid='%[1]s')`, entity))
}

func setReviewedTitle(t *testing.T, repo models.Repository, entity, title string) {
	t.Helper()
	state := metadataState(t, repo, entity, "title")
	_, err := applyMetadata(repo, metadataInput(state, "set", "review", fmt.Sprintf("%q", title)), false)
	require.NoError(t, err)
}

func TestDeletingMediaRetiresItsFieldChoices(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	deleted := archiveFind(t, repo, models.ArchiveScene, 31).UUID
	kept := archiveFind(t, repo, models.ArchiveScene, 32).UUID
	image := archiveFind(t, repo, models.ArchiveImage, 41).UUID
	setReviewedTitle(t, repo, deleted, "Deleted scene")
	setReviewedTitle(t, repo, kept, "Kept scene")
	setReviewedTitle(t, repo, image, "Deleted image")
	// A relationship edit records a performers choice with references.
	attachmentSQL(t, db, "INSERT INTO performers_scenes(performer_id, scene_id) VALUES (72, 31)")
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.NotZero(t, queryUint(t, raw, fmt.Sprintf(`SELECT count(*) FROM metadata_field_references r
 JOIN metadata_field_decisions d ON d.uuid=r.decision_uuid WHERE d.entity_uuid='%s'`, deleted)))
	keptRecords := fieldRecordCount(t, raw, kept)
	require.NotZero(t, keptRecords)

	attachmentSQL(t, db, "DELETE FROM scenes WHERE id=31; DELETE FROM images WHERE id=41")

	for _, id := range []string{deleted, image} {
		require.Equal(t, models.ArchiveEntityDeleted, archiveIdentity(t, repo, id).State)
		require.Zero(t, fieldRecordCount(t, raw, id))
		require.Empty(t, metadataHistory(t, repo, id, "title"))
	}
	require.Equal(t, keptRecords, fieldRecordCount(t, raw, kept))
	require.JSONEq(t, `"Kept scene"`, string(metadataHistory(t, repo, kept, "title")[0].Value))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

// Schema 1000146 removes the field choices that earlier deletions left behind.
func TestDeletedMediaFieldChoicesRetirement(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "deleted-media.sqlite")
	writeEmptyNativeFixture(t, path)
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	repo := db.Repository()
	attachmentSQL(t, db, archiveIdentityFixture)
	deleted := archiveFind(t, repo, models.ArchiveScene, 31).UUID
	kept := archiveFind(t, repo, models.ArchiveScene, 32).UUID
	setReviewedTitle(t, repo, deleted, "Deleted scene")
	setReviewedTitle(t, repo, kept, "Kept scene")
	require.NoError(t, db.Close())

	raw := openRawDB(t, path)
	defer raw.Close()
	removeDeletedMediaFieldsSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000145,dirty=0")
	require.NoError(t, err)
	_, err = raw.Exec("DELETE FROM scenes WHERE id=31")
	require.NoError(t, err)
	choices := queryUint(t, raw, fmt.Sprintf("SELECT count(*) FROM metadata_field_decisions WHERE entity_uuid='%s'", deleted))
	baselines := queryUint(t, raw, fmt.Sprintf("SELECT count(*) FROM metadata_field_baselines WHERE entity_uuid='%s'", deleted))
	require.NotZero(t, choices)
	keptRecords := fieldRecordCount(t, raw, kept)

	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, db.Open(path), &needed)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.Open(path))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.AuditForTesting(path))

	require.Equal(t, models.ArchiveEntityDeleted, archiveIdentity(t, repo, deleted).State)
	require.Zero(t, fieldRecordCount(t, raw, deleted))
	require.Equal(t, keptRecords, fieldRecordCount(t, raw, kept))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	var details string
	require.NoError(t, raw.QueryRow("SELECT details FROM native_migration_history WHERE version=1000146").Scan(&details))
	require.JSONEq(t, fmt.Sprintf(`{"items":1,"choices":%d,"baselines":%d}`, choices, baselines), details)
}
