package sqlite_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeMetadataFileKeepsSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	var exists bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM native_migration_history WHERE version=1000094)").Scan(&exists))
	if !exists {
		return
	}
	_, err := raw.Exec(`DROP TRIGGER metadata_file_edit_apply_request_distinct;
DROP TABLE metadata_file_edit_keeps; DELETE FROM native_migration_history WHERE version=1000094`)
	require.NoError(t, err)
}

func keepFileEditRequest(t *testing.T, repo models.Repository, input models.MetadataFileEditInput) models.MetadataFileEditApplyInput {
	t.Helper()
	preview := previewFileEdit(t, repo, input)
	return models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest, KeepCurrent: true}
}

func TestMetadataFileKeepPreservesValuesProtectionAndUnreviewedFields(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Catalog title","details":null,"actors":["Unknown performer"],"mystery":123}`)
	attachmentSQL(t, f.db, "UPDATE scenes SET title='Curated',details='Keep details' WHERE id=31")
	before := metadataState(t, f.repo, input.EntityUUID, "title")
	entity := archiveFind(t, f.repo, models.ArchiveScene, 31)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	tables := []string{"scenes", "archive_entities", "metadata_field_decisions", "metadata_field_heads", "source_file_history_edits", "source_file_matches"}
	rows := make(map[string][][]any)
	for _, table := range tables {
		rows[table] = albumJobRows(t, raw, table)
	}
	require.Len(t, archiveQueue(t, f.repo, "metadata", "", 25).Items, 1)
	for _, field := range []string{"title", "details", "actors", "mystery"} {
		input.SourceField = field
		preview := previewFileEdit(t, f.repo, input)
		switch field {
		case "actors":
			require.Equal(t, "unresolved_names", preview.Status)
		case "mystery":
			require.Equal(t, "unsupported", preview.Status)
		}
		receipt, replayed, err := applyFileEdit(f.repo, keepFileEditRequest(t, f.repo, input))
		require.NoError(t, err)
		require.False(t, replayed)
		require.True(t, receipt.KeptCurrent)
		require.True(t, receipt.Request.KeepCurrent)
		require.Empty(t, receipt.DecisionUUID)
		if field != "mystery" {
			require.Len(t, archiveQueue(t, f.repo, "metadata", "", 25).Items, 1, "other retained fields still need review")
		}
	}
	require.Empty(t, archiveQueue(t, f.repo, "metadata", "", 25).Items)
	require.Equal(t, before, metadataState(t, f.repo, input.EntityUUID, "title"))
	require.Equal(t, entity, archiveFind(t, f.repo, models.ArchiveScene, 31))
	for _, table := range tables {
		require.Equal(t, rows[table], albumJobRows(t, raw, table), table)
	}
	require.EqualValues(t, 4, queryUint(t, raw, "SELECT count(*) FROM metadata_file_edit_keeps"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_file_edit_reviews"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestMetadataFileKeepReplaysAfterEditsAdoptionDeletionAndRestart(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Catalog"}`)
	// A previous decision is retained as review context, not replaced by Keep.
	_, err := applyMetadata(f.repo, metadataInput(metadataState(t, f.repo, input.EntityUUID, "title"), "inherit", "review", ""), false)
	require.NoError(t, err)
	request := keepFileEditRequest(t, f.repo, input)
	first, replay, err := applyFileEdit(f.repo, request)
	require.NoError(t, err)
	require.False(t, replay)
	attachmentSQL(t, f.db, "UPDATE scenes SET title='Newer library edit' WHERE id=31")
	require.Empty(t, archiveQueue(t, f.repo, "metadata", "", 25).Items)
	changed := request
	changed.KeepCurrent = false
	_, _, err = applyFileEdit(f.repo, changed)
	require.ErrorIs(t, err, models.ErrMetadataFileReviewReplay)
	current := archiveFind(t, f.repo, models.ArchiveScene, 31)
	adopted := uuid.NewString()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveEntity.AdoptUUID(ctx, current.UUID, adopted, current.Revision)
		return err
	}))
	require.Empty(t, archiveQueue(t, f.repo, "metadata", "", 25).Items)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Destroy(ctx, 21) }))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	second, replay, err := applyFileEdit(f.repo, request)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, first, second)
	require.Equal(t, `"Newer library edit"`, string(metadataState(t, f.repo, adopted, "title").Value))
}

func TestMetadataFileKeepCannotReuseAnApplyRequest(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Catalog"}`)
	request := keepFileEditRequest(t, f.repo, input)
	request.KeepCurrent = false
	_, _, err := applyFileEdit(f.repo, request)
	require.NoError(t, err)
	request.KeepCurrent = true
	_, _, err = applyFileEdit(f.repo, request)
	require.ErrorIs(t, err, models.ErrMetadataFileReviewReplay)
}

func TestMetadataFileKeepRemainsReviewedAfterSceneMerge(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Catalog"}`)
	receipt, _, err := applyFileEdit(f.repo, keepFileEditRequest(t, f.repo, input))
	require.NoError(t, err)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := f.repo.Scene.RedirectMergedIdentities(ctx, []int{31}, 32); err != nil {
			return err
		}
		_, _, err := f.db.ExecSQL(ctx, "UPDATE scenes_files SET scene_id=32 WHERE scene_id=31", nil)
		if err != nil {
			return err
		}
		return f.repo.Scene.Destroy(ctx, 31)
	}))
	require.Empty(t, archiveQueue(t, f.repo, "metadata", "", 25).Items)
	replayed, replay, err := applyFileEdit(f.repo, receipt.Request)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, receipt, replayed)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestMetadataFileKeepRejectsStaleScopeWithoutReceipt(t *testing.T) {
	for _, change := range []string{
		"UPDATE scenes SET title='Later title' WHERE id=31",
		"UPDATE scenes SET details='Later details' WHERE id=31",
		"UPDATE files SET size=size+1 WHERE id=21",
		"DELETE FROM scenes_files WHERE scene_id=31",
	} {
		t.Run(change, func(t *testing.T) {
			f, input := metadataFileReviewFixture(t, `{"title":"Catalog"}`)
			request := keepFileEditRequest(t, f.repo, input)
			attachmentSQL(t, f.db, change)
			before := metadataState(t, f.repo, input.EntityUUID, "title")
			_, _, err := applyFileEdit(f.repo, request)
			require.ErrorIs(t, err, models.ErrMetadataFieldConflict)
			require.Equal(t, before, metadataState(t, f.repo, input.EntityUUID, "title"))
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_file_edit_keeps"))
		})
	}
}

func TestMetadataFileKeepLateFailureRollsBackEvenWhenCaught(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Catalog"}`)
	request := keepFileEditRequest(t, f.repo, input)
	attachmentSQL(t, f.db, `CREATE TRIGGER lose_keep_receipt AFTER INSERT ON metadata_file_edit_keeps BEGIN DELETE FROM metadata_file_edit_keeps WHERE request_uuid=NEW.request_uuid; END`)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := f.repo.MetadataField.ApplyFileEdit(ctx, request)
		require.ErrorIs(t, err, models.ErrSourcePayloadCorrupt)
		_, _, err = f.db.ExecSQL(ctx, "UPDATE scenes SET title='Must roll back' WHERE id=31", nil)
		require.NoError(t, err)
		return nil
	})
	require.ErrorIs(t, err, models.ErrMetadataFileReviewInvalid)
	require.NotEqual(t, `"Must roll back"`, string(metadataState(t, f.repo, input.EntityUUID, "title").Value))
}

func TestMetadataFileKeepRejectsCorruptReceiptBeforeWrites(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Catalog"}`)
	_, _, err := applyFileEdit(f.repo, keepFileEditRequest(t, f.repo, input))
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='metadata_file_edit_keep_immutable'").Scan(&guard))
	_, err = raw.Exec("DROP TRIGGER metadata_file_edit_keep_immutable; UPDATE metadata_file_edit_keeps SET signature=printf('%064d',0);" + guard)
	require.NoError(t, err)
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.ErrorIs(t, f.db.Open(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestMetadataFileKeepsMigrationPreservesExistingReviewsAndFields(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Applied"}`)
	request := keepFileEditRequest(t, f.repo, input)
	request.KeepCurrent = false
	first, _, err := applyFileEdit(f.repo, request)
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := albumJobRows(t, raw, "scenes")
	reviews := albumJobRows(t, raw, "metadata_file_edit_reviews")
	removeMetadataFileKeepsSchema(t, raw)
	_, err = raw.Exec("UPDATE schema_migrations SET version=1000093,dirty=0")
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, f.db.Open(f.db.DatabasePath()), &needed)
	require.NoError(t, f.db.RunAllMigrations())
	require.Equal(t, before, albumJobRows(t, raw, "scenes"))
	require.Equal(t, reviews, albumJobRows(t, raw, "metadata_file_edit_reviews"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_file_edit_keeps"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	second, replay, err := applyFileEdit(f.repo, request)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, first, second)
}

func TestMetadataFileKeepsMigrationRejectsForeignCollisionWithoutWrites(t *testing.T) {
	f, _ := metadataFileReviewFixture(t, `{"title":"Unselected"}`)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	removeMetadataFileKeepsSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000093,dirty=0; CREATE TABLE metadata_file_edit_keeps(private_data TEXT); INSERT INTO metadata_file_edit_keeps VALUES('keep')")
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, f.db.Open(f.db.DatabasePath()), &needed)
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Error(t, f.db.RunAllMigrations())
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestMetadataFileKeepsAnonymisationRemovesPrivateReviewEvidence(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Private old title"}`)
	receipt, _, err := applyFileEdit(f.repo, keepFileEditRequest(t, f.repo, input))
	require.NoError(t, err)
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	contents, err := os.ReadFile(output)
	require.NoError(t, err)
	require.NotContains(t, string(contents), "Private old title")
	require.NotContains(t, string(contents), receipt.RequestUUID)
	raw := openRawDB(t, output)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_file_edit_keeps"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	replayed, replay, err := applyFileEdit(f.repo, receipt.Request)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, receipt, replayed)
}
