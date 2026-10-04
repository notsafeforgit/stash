package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func metadataFileReviewFixture(t *testing.T, edits string) (sourceFileFixture, models.MetadataFileEditInput) {
	t.Helper()
	f, history := fileHistoryFixture(t)
	var err error
	history.Edits, err = archive.CatalogFileEdits([]byte(edits))
	require.NoError(t, err)
	recordFileHistory(t, f.repo, history)
	attachmentSQL(t, f.db, `INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(31,21,1)`)
	file := archiveFind(t, f.repo, models.ArchiveFile, 21)
	entity := archiveFind(t, f.repo, models.ArchiveScene, 31)
	match := recordFileMatch(t, f.repo, models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: f.observation.UUID,
		FileUUID: file.UUID, Generation: 1, LibraryRootPath: "/identity-fixture", Basis: "exact-path", Origin: "migration"})
	return f, models.MetadataFileEditInput{EntityUUID: entity.UUID, HistoryUUID: history.UUID, SourceField: "title", MatchUUID: match.UUID}
}

func previewFileEdit(t *testing.T, repo models.Repository, input models.MetadataFileEditInput) *models.MetadataFileEditPreview {
	t.Helper()
	var ret *models.MetadataFileEditPreview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.MetadataField.PreviewFileEdit(ctx, input)
		return err
	}))
	return ret
}

func applyFileEdit(repo models.Repository, input models.MetadataFileEditApplyInput) (*models.MetadataFileEditReview, bool, error) {
	var ret *models.MetadataFileEditReview
	var replayed bool
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, replayed, err = repo.MetadataField.ApplyFileEdit(ctx, input)
		return err
	})
	return ret, replayed, err
}

func TestMetadataFileReviewKeepsChoicesAndReplaysOriginalReceipt(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Catalog choice","details":null,"urls":[" https://example.test/post ","https://example.test/post"]}`)
	attachmentSQL(t, f.db, `UPDATE scenes SET title='Curated library title',details='Keep details' WHERE id=31`)
	before := metadataState(t, f.repo, input.EntityUUID, "title")
	preview := previewFileEdit(t, f.repo, input)
	require.Equal(t, "ready", preview.Status)
	require.True(t, preview.Protected)
	require.Equal(t, before.Value, preview.CurrentValue)
	require.Equal(t, before, metadataState(t, f.repo, input.EntityUUID, "title"), "preview must not modify selected fields or history")
	request := models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
	first, replay, err := applyFileEdit(f.repo, request)
	require.NoError(t, err)
	require.False(t, replay)
	state := metadataState(t, f.repo, input.EntityUUID, "title")
	require.Equal(t, `"Catalog choice"`, string(state.Value))
	require.Equal(t, first, state.Decision.FileEdit)
	require.True(t, state.Protected)
	attachmentSQL(t, f.db, "UPDATE scenes SET title='Newer choice' WHERE id=31")
	second, replay, err := applyFileEdit(f.repo, request)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, first, second)
	require.Equal(t, `"Newer choice"`, string(metadataState(t, f.repo, input.EntityUUID, "title").Value))
	request.SourceField = "details"
	_, _, err = applyFileEdit(f.repo, request)
	require.ErrorIs(t, err, models.ErrMetadataFileReviewReplay)
	for _, field := range []string{"details", "urls"} {
		input.SourceField = field
		preview = previewFileEdit(t, f.repo, input)
		_, _, err = applyFileEdit(f.repo, models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
		require.NoError(t, err)
	}
	details := metadataState(t, f.repo, input.EntityUUID, "details")
	require.Equal(t, "inherit", details.Mode)
	require.Equal(t, `"Keep details"`, string(details.Value))
	require.False(t, details.Protected)
	require.JSONEq(t, `["https://example.test/post"]`, string(metadataState(t, f.repo, input.EntityUUID, "urls").Value))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, details, metadataState(t, f.repo, input.EntityUUID, "details"))
}

func TestMetadataFileReviewFencesChangedEntityFileOwnershipAndRelationships(t *testing.T) {
	for _, change := range []string{"entity", "file", "ownership", "match", "performer"} {
		t.Run(change, func(t *testing.T) {
			f, input := metadataFileReviewFixture(t, `{"title":"Catalog","actors":["A name"]}`)
			if change == "performer" {
				attachmentSQL(t, f.db, `INSERT INTO performers(id,created_at,updated_at) VALUES(70,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO performer_names(performer_id,name,position) VALUES(70,'A name',0)`)
				input.SourceField = "actors"
			}
			preview := previewFileEdit(t, f.repo, input)
			switch change {
			case "entity":
				attachmentSQL(t, f.db, "UPDATE scenes SET title='Later edit' WHERE id=31")
			case "file":
				attachmentSQL(t, f.db, "UPDATE files SET size=size+1 WHERE id=21")
			case "ownership":
				attachmentSQL(t, f.db, "DELETE FROM scenes_files WHERE scene_id=31")
			case "match":
				input.MatchUUID = uuid.NewString()
			case "performer":
				attachmentSQL(t, f.db, "UPDATE performers SET disambiguation='Changed' WHERE id=70")
			}
			_, _, err := applyFileEdit(f.repo, models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
			require.Error(t, err)
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_file_edit_reviews"))
		})
	}
}

func TestMetadataFileReviewAmbiguousAliasesRequireExplicitSelection(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"actors":["Shared","Unmatched"],"mystery":12345}`)
	attachmentSQL(t, f.db, `UPDATE performer_names SET name='Shared' WHERE performer_id=71 AND position=0;
 UPDATE performer_names SET name='Canonical' WHERE performer_id=72 AND position=0;
 INSERT INTO performer_names(performer_id,name,position,ignore_auto_tag) VALUES(72,'Shared',1,0);`)
	input.SourceField = "actors"
	preview := previewFileEdit(t, f.repo, input)
	require.Equal(t, "unresolved_names", preview.Status)
	require.Len(t, preview.Names, 2)
	require.Len(t, preview.Names[0].Candidates, 2)
	require.ElementsMatch(t, []int{71, 72}, []int{preview.Names[0].Candidates[0].LocalID, preview.Names[0].Candidates[1].LocalID})
	require.Nil(t, preview.Names[0].Selected)
	require.Nil(t, preview.Value)
	_, _, err := applyFileEdit(f.repo, models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
	require.ErrorIs(t, err, models.ErrMetadataFieldConflict)
	selected := archiveFind(t, f.repo, models.ArchivePerformer, 72)
	input.Selections = map[string]models.MetadataNameSelection{"Shared": {UUID: selected.UUID, Revision: selected.Revision}, "Unmatched": {UUID: selected.UUID, Revision: selected.Revision}}
	preview = previewFileEdit(t, f.repo, input)
	require.Equal(t, "ready", preview.Status)
	require.Equal(t, 72, preview.Names[0].Selected.LocalID)
	require.JSONEq(t, fmt.Sprintf(`[%q]`, selected.UUID), string(preview.Value), "several aliases for one performer must not duplicate attribution")
	_, _, err = applyFileEdit(f.repo, models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
	require.NoError(t, err)
	input.SourceField, input.Selections = "mystery", nil
	require.Equal(t, "unsupported", previewFileEdit(t, f.repo, input).Status)
}

func TestMetadataFileReviewLateFailureCannotCommitFieldWithoutReceipt(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Catalog"}`)
	preview := previewFileEdit(t, f.repo, input)
	attachmentSQL(t, f.db, `CREATE TRIGGER reject_file_review BEFORE INSERT ON metadata_file_edit_reviews BEGIN SELECT RAISE(ABORT,'receipt failure'); END`)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := f.repo.MetadataField.ApplyFileEdit(ctx, models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
		require.ErrorContains(t, err, "receipt failure")
		return nil // Even an accidentally swallowed late error cannot commit.
	})
	require.ErrorIs(t, err, models.ErrMetadataFileReviewInvalid)
	require.Equal(t, preview.CurrentValue, metadataState(t, f.repo, input.EntityUUID, "title").Value)
}

func TestMetadataFileReviewCorruptionIsRejectedWithoutWrites(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"title":"Catalog"}`)
	preview := previewFileEdit(t, f.repo, input)
	_, _, err := applyFileEdit(f.repo, models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='metadata_file_edit_review_immutable'").Scan(&guard))
	_, err = raw.Exec("DROP TRIGGER metadata_file_edit_review_immutable; UPDATE metadata_file_edit_reviews SET signature=printf('%064d',0);" + guard)
	require.NoError(t, err)
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.ErrorIs(t, f.db.Open(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func removeMetadataFileReviewSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec("DROP TABLE metadata_file_edit_reviews; DELETE FROM native_migration_history WHERE version=1000067")
	require.NoError(t, err)
}

func TestMetadataFileReviewMigrationPreservesFieldsAndSourceEvidence(t *testing.T) {
	f, _ := metadataFileReviewFixture(t, `{"title":"Unselected"}`)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := albumJobRows(t, raw, "scenes")
	evidence := albumJobRows(t, raw, "source_file_history_edits")
	removeMetadataFileReviewSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000066,dirty=0")
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, f.db.Open(f.db.DatabasePath()), &needed)
	require.NoError(t, f.db.RunAllMigrations())
	require.Equal(t, before, albumJobRows(t, raw, "scenes"))
	require.Equal(t, evidence, albumJobRows(t, raw, "source_file_history_edits"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_file_edit_reviews"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestMetadataFileReviewUsesImportedAlternativesAndBoundedDiscovery(t *testing.T) {
	f := fileHistoryImportFixture(t, 0, nil)
	advanceFileHistory(t, f, 0)
	entity := archiveFind(t, f.repo, models.ArchiveScene, 31)
	before := metadataState(t, f.repo, entity.UUID, "title")
	afterHistory, afterMatch := "", ""
	seen := map[string]bool{}
	var exact, duplicate *models.MetadataFileEditPreview
	for {
		var page []models.MetadataFileEditCandidate
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			page, err = f.repo.MetadataField.FileEdits(ctx, entity.UUID, afterHistory, afterMatch, 1)
			return err
		}))
		if len(page) == 0 {
			break
		}
		require.Len(t, page, 1)
		item := page[0]
		require.False(t, seen[item.HistoryUUID+item.MatchUUID])
		seen[item.HistoryUUID+item.MatchUUID] = true
		afterHistory, afterMatch = item.HistoryUUID, item.MatchUUID
		preview := previewFileEdit(t, f.repo, models.MetadataFileEditInput{EntityUUID: entity.UUID, HistoryUUID: item.HistoryUUID, MatchUUID: item.MatchUUID, SourceField: "title"})
		require.Equal(t, "ready", preview.Status)
		switch string(preview.Value) {
		case `"Exact path title"`:
			exact = preview
		case `"Later duplicate title"`:
			duplicate = preview
		}
	}
	require.Len(t, seen, 3, "different source edits must remain separate alternatives")
	require.NotNil(t, exact)
	require.NotNil(t, duplicate)
	require.Equal(t, exact.FileUUID, duplicate.FileUUID, "a surviving file can retain edits from more than one source path")
	require.Equal(t, before, metadataState(t, f.repo, entity.UUID, "title"))
	_, _, err := applyFileEdit(f.repo, models.MetadataFileEditApplyInput{MetadataFileEditInput: exact.Input, RequestUUID: uuid.NewString(), Digest: exact.Digest})
	require.NoError(t, err)
	require.Equal(t, `"Exact path title"`, string(metadataState(t, f.repo, entity.UUID, "title").Value))
	other := archiveFind(t, f.repo, models.ArchiveScene, 32)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		page, err := f.repo.MetadataField.FileEdits(ctx, other.UUID, "", "", 100)
		require.NoError(t, err)
		require.Empty(t, page)
		_, err = f.repo.MetadataField.FileEdits(ctx, entity.UUID, afterHistory, "", 1)
		require.ErrorIs(t, err, models.ErrMetadataFileReviewInvalid)
		_, err = f.repo.MetadataField.FileEdits(ctx, entity.UUID, "", "", 101)
		require.ErrorIs(t, err, models.ErrMetadataFileReviewInvalid)
		return nil
	}))
}

func TestMetadataFileReviewRelationshipsAdoptionAndDeletion(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"studio":"Identity studio A","tags":["Identity tag A"],"movie":"Identity group A","title":"Reviewed"}`)
	attachmentSQL(t, f.db, archiveMetadataFixture)
	for _, sourceField := range []string{"studio", "tags", "movie"} {
		input.SourceField = sourceField
		preview := previewFileEdit(t, f.repo, input)
		require.Equal(t, "ready", preview.Status)
		require.Len(t, preview.ReferenceRevisions, 1)
		_, _, err := applyFileEdit(f.repo, models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
		require.NoError(t, err)
	}
	input.SourceField = "title"
	preview := previewFileEdit(t, f.repo, input)
	request := models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
	first, _, err := applyFileEdit(f.repo, request)
	require.NoError(t, err)
	current := archiveFind(t, f.repo, models.ArchiveScene, 31)
	adopted := uuid.NewString()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveEntity.AdoptUUID(ctx, current.UUID, adopted, current.Revision)
		return err
	}))
	require.Equal(t, first, metadataState(t, f.repo, adopted, "title").Decision.FileEdit)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Destroy(ctx, 21) }))
	second, replay, err := applyFileEdit(f.repo, request)
	require.NoError(t, err)
	require.True(t, replay)
	require.Equal(t, first, second)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestMetadataFileReviewImageArchiveGenerationAndUnsupportedSceneField(t *testing.T) {
	f, history := fileHistoryFixture(t)
	attachmentSQL(t, f.db, `INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES(22,1,'album.zip',9999,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO folders(id,path,basename,zip_file_id,mod_time,created_at,updated_at) VALUES(2,'/identity-fixture/album.zip','album.zip',22,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO files(id,parent_folder_id,basename,zip_file_id,size,mod_time,created_at,updated_at) VALUES(23,2,'member.jpg',22,1000,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO images_files(image_id,file_id,"primary") VALUES(41,23,1);`)
	zipPath := "album.zip"
	observation := f.observation
	observation.UUID, observation.ArchivePath, observation.RelativePath = uuid.NewString(), &zipPath, "member.jpg"
	recordFileObservation(t, f.repo, observation)
	history.Locations = []models.SourceFileHistoryLocation{{RelativePath: observation.RelativePath, ArchivePath: &zipPath, ObservationUUID: &observation.UUID}}
	var err error
	history.Edits, err = archive.CatalogFileEdits([]byte(`{"title":"An album image","director":"Scene only"}`))
	require.NoError(t, err)
	recordFileHistory(t, f.repo, history)
	member := archiveFind(t, f.repo, models.ArchiveFile, 23)
	container := archiveFind(t, f.repo, models.ArchiveFile, 22)
	entity := archiveFind(t, f.repo, models.ArchiveImage, 41)
	generation := int64(1)
	match := recordFileMatch(t, f.repo, models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: observation.UUID, FileUUID: member.UUID, Generation: 1,
		ArchiveFileUUID: &container.UUID, ArchiveGeneration: &generation, LibraryRootPath: "/identity-fixture", Basis: "exact-path", Origin: "migration"})
	input := models.MetadataFileEditInput{EntityUUID: entity.UUID, HistoryUUID: history.UUID, MatchUUID: match.UUID, SourceField: "director"}
	require.Equal(t, "unsupported", previewFileEdit(t, f.repo, input).Status)
	input.SourceField = "title"
	preview := previewFileEdit(t, f.repo, input)
	require.Equal(t, "ready", preview.Status)
	_, _, err = applyFileEdit(f.repo, models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
	require.NoError(t, err)
	preview = previewFileEdit(t, f.repo, input)
	attachmentSQL(t, f.db, "UPDATE files SET size=size+1 WHERE id=22")
	_, _, err = applyFileEdit(f.repo, models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
	require.ErrorIs(t, err, models.ErrFileGenerationConflict)
	require.ErrorIs(t, err, models.ErrMetadataFieldConflict)
}

func TestMetadataFileReviewCandidateOverflowCannotLookUnique(t *testing.T) {
	f, input := metadataFileReviewFixture(t, `{"actors":["Shared identity"]}`)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for i := 200; i < 301; i++ {
			if _, _, err := f.db.ExecSQL(ctx, "INSERT INTO performers(id,created_at,updated_at) VALUES(?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)", []interface{}{i}); err != nil {
				return err
			}
			if _, _, err := f.db.ExecSQL(ctx, "INSERT INTO performer_names(performer_id,name,position) VALUES(?,'Shared identity',0)", []interface{}{i}); err != nil {
				return err
			}
		}
		return nil
	}))
	input.SourceField = "actors"
	preview := previewFileEdit(t, f.repo, input)
	require.Equal(t, "unresolved_names", preview.Status)
	require.Len(t, preview.Names, 1)
	require.Len(t, preview.Names[0].Candidates, 100)
	require.True(t, preview.Names[0].More)
	require.Nil(t, preview.Names[0].Selected)
	selected := preview.Names[0].Candidates[99]
	input.Selections = map[string]models.MetadataNameSelection{"Shared identity": {UUID: selected.UUID, Revision: selected.Revision}}
	preview = previewFileEdit(t, f.repo, input)
	require.Equal(t, "ready", preview.Status)
	_, _, err := applyFileEdit(f.repo, models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest})
	require.NoError(t, err)
}

func TestMetadataFileReviewMigrationCollisionPreservesOriginalDatabase(t *testing.T) {
	f, _ := metadataFileReviewFixture(t, `{"title":"Unselected"}`)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	removeMetadataFileReviewSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000066,dirty=0; CREATE TABLE metadata_file_edit_reviews(private_data TEXT); INSERT INTO metadata_file_edit_reviews VALUES('keep')")
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
