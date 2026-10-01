package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func metadataState(t *testing.T, repo models.Repository, entity, field string) *models.MetadataFieldState {
	t.Helper()
	var ret *models.MetadataFieldState
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.MetadataField.State(ctx, entity, field)
		return err
	}))
	return ret
}

func metadataHistory(t *testing.T, repo models.Repository, entity, field string) []models.MetadataFieldDecision {
	t.Helper()
	var ret []models.MetadataFieldDecision
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.MetadataField.History(ctx, entity, field, 0, 100)
		return err
	}))
	return ret
}

func applyMetadata(repo models.Repository, input models.MetadataFieldDecisionInput, automatic bool) (*models.MetadataFieldState, error) {
	var ret *models.MetadataFieldState
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		if automatic {
			ret, err = repo.MetadataField.ApplyAutomatic(ctx, input)
		} else {
			ret, err = repo.MetadataField.Decide(ctx, input)
		}
		return err
	})
	return ret, err
}

func metadataInput(state *models.MetadataFieldState, mode, origin, value string) models.MetadataFieldDecisionInput {
	return models.MetadataFieldDecisionInput{EntityUUID: state.Entity.UUID, ExpectedEntityRevision: state.Entity.Revision,
		Field: state.Field, Mode: mode, Origin: origin, Value: json.RawMessage(value)}
}

func TestMetadataFieldChoicesClearInheritAndReplay(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, "INSERT INTO scenes(id, created_at, updated_at) VALUES (33, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
	identity := archiveFind(t, repo, models.ArchiveScene, 33)
	state := metadataState(t, repo, identity.UUID, "title")
	require.Equal(t, "inherit", state.Mode)
	require.Equal(t, "unset", state.Origin)
	require.False(t, state.Protected)
	require.Nil(t, state.Decision)

	state, err := applyMetadata(repo, metadataInput(state, "inherit", "filename", `"My purchased file"`), true)
	require.NoError(t, err)
	require.False(t, state.Protected)
	require.Equal(t, "filename", state.Origin)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "metadata-post"}, "")
	capture := recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 1, "Source caption"))
	input := metadataInput(state, "inherit", "source", `"Source caption"`)
	input.CaptureUUID = capture.UUID
	state, err = applyMetadata(repo, input, true)
	require.NoError(t, err)
	require.Equal(t, capture.UUID, *state.Decision.CaptureUUID)
	require.JSONEq(t, `"Source caption"`, string(state.Value))
	_, err = applyMetadata(repo, input, true)
	require.ErrorIs(t, err, models.ErrMetadataFieldConflict)
	input.ExpectedEntityRevision = state.Entity.Revision
	replayed, err := applyMetadata(repo, input, true)
	require.NoError(t, err)
	require.Equal(t, state, replayed)
	require.Len(t, metadataHistory(t, repo, identity.UUID, "title"), 2)
	_, err = applyMetadata(repo, metadataInput(state, "inherit", "filename", `"Weaker filename"`), true)
	require.ErrorIs(t, err, models.ErrMetadataFieldProtected)

	state, err = applyMetadata(repo, metadataInput(state, "clear", "review", ""), false)
	require.NoError(t, err)
	require.True(t, state.Protected)
	require.Equal(t, "clear", state.Mode)
	require.Equal(t, `""`, string(state.Value))
	_, err = applyMetadata(repo, metadataInput(state, "inherit", "filename", `"Must stay empty"`), true)
	require.ErrorIs(t, err, models.ErrMetadataFieldProtected)
	state, err = applyMetadata(repo, metadataInput(state, "inherit", "review", ""), false)
	require.NoError(t, err)
	require.False(t, state.Protected)
	state, err = applyMetadata(repo, metadataInput(state, "inherit", "filename", `"Filename again"`), true)
	require.NoError(t, err)
	require.Equal(t, `"Filename again"`, string(state.Value))

	// Existing entity-side updates also protect the field, without a plugin or
	// dependency on the new API. Reaffirming an already-empty value is a choice.
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.Scene.UpdatePartial(ctx, 33, models.ScenePartial{Title: models.NewOptionalString("")})
		return err
	}))
	state = metadataState(t, repo, identity.UUID, "title")
	require.Equal(t, "library", state.Origin)
	require.True(t, state.Protected)
	state, err = applyMetadata(repo, metadataInput(state, "inherit", "review", ""), false)
	require.NoError(t, err)
	require.False(t, state.Protected)
	attachmentSQL(t, db, "UPDATE scenes SET title='' WHERE id=33")
	state = metadataState(t, repo, identity.UUID, "title")
	require.Equal(t, "library", state.Origin)
	require.Equal(t, "clear", state.Mode)
	require.True(t, state.Protected)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.Equal(t, state, metadataState(t, repo, identity.UUID, "title"))
}

func TestMetadataFieldSchemasAndOrdinaryEdits(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	targets := map[models.ArchiveEntityKind]*models.ArchiveEntity{
		models.ArchivePerformer: archiveFind(t, repo, models.ArchivePerformer, 71),
		models.ArchiveTag:       archiveFind(t, repo, models.ArchiveTag, 81),
		models.ArchiveStudio:    archiveFind(t, repo, models.ArchiveStudio, 91),
		models.ArchiveGroup:     archiveFind(t, repo, models.ArchiveGroup, 101),
	}
	gallery := createArchiveGallery(t, repo, "Existing gallery")
	for _, entity := range []*models.ArchiveEntity{
		archiveFind(t, repo, models.ArchiveScene, 31), archiveFind(t, repo, models.ArchiveImage, 41), archiveFind(t, repo, models.ArchiveGallery, gallery.ID),
	} {
		for _, def := range models.MetadataFields(entity.Kind) {
			state := metadataState(t, repo, entity.UUID, def.Name)
			var value string
			switch def.Type {
			case "string":
				value = `"A caption with <markup> & quotes \" and 日本語"`
			case "date":
				value = `"2026-09"`
			case "integer":
				value = `97`
			case "boolean":
				value = `true`
			case "urls":
				value = `["https://example.test/post/1","https://example.test/post/2"]`
			case "custom_fields":
				value = `{"note":"Purchased file","score":0.3333333333333333,"empty":""}`
			case "reference":
				value = `"` + targets[def.ReferenceKind].UUID + `"`
			case "references":
				value = `["` + targets[def.ReferenceKind].UUID + `"]`
			case "groups":
				value = `[{"uuid":"` + targets[def.ReferenceKind].UUID + `","scene_index":2}]`
			}
			input := metadataInput(state, "set", "review", value)
			if target := targets[def.ReferenceKind]; target != nil {
				input.ReferenceRevisions = map[string]int{target.UUID: target.Revision}
			}
			selected, err := applyMetadata(repo, input, false)
			require.NoError(t, err, "%s.%s", entity.Kind, def.Name)
			require.JSONEq(t, value, string(selected.Value))
			require.True(t, selected.Protected)
			cleared, err := applyMetadata(repo, metadataInput(selected, "clear", "review", ""), false)
			require.NoError(t, err)
			require.JSONEq(t, string(def.ClearValue), string(cleared.Value))
			require.True(t, cleared.Protected)
		}
	}
	attachmentSQL(t, db, "UPDATE scenes SET date='2025-01-01', date_precision=2, details='Ordinary edit' WHERE id=31")
	identity := archiveFind(t, repo, models.ArchiveScene, 31)
	date := metadataState(t, repo, identity.UUID, "date")
	require.Equal(t, `"2025"`, string(date.Value))
	require.Equal(t, "library", date.Origin)
	state := metadataState(t, repo, identity.UUID, "title")
	for _, tc := range []struct{ field, mode, value string }{
		{"title", "set", `42`}, {"title", "set", `null`}, {"title", "set", `{"settings":true}`},
		{"title", "clear", `"not allowed"`}, {"title", "unknown", `"text"`},
		{"rating100", "set", `100.1`}, {"rating100", "set", `101`}, {"rating100", "set", `-1`},
		{"organized", "set", `1`}, {"date", "set", `"2026-02-31"`}, {"date", "set", `[]`},
		{"id", "set", `31`}, {"o_counter", "set", `10`}, {"file_ids", "set", `[21]`}, {"photographer", "set", `"Not a scene field"`},
	} {
		input := metadataInput(state, tc.mode, "review", tc.value)
		input.Field = tc.field
		_, err := applyMetadata(repo, input, false)
		require.Error(t, err, "%s=%s", tc.field, tc.value)
	}
	require.Equal(t, state.Entity.Revision, archiveFind(t, repo, models.ArchiveScene, 31).Revision)
	rating := metadataState(t, repo, identity.UUID, "rating100")
	rating, err := applyMetadata(repo, metadataInput(rating, "set", "review", "-0"), false)
	require.NoError(t, err)
	require.Equal(t, "0", string(rating.Value))
}

func TestMetadataFieldPreservedValuesAndPortableHistory(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	identity := archiveFind(t, repo, models.ArchiveImage, 41)
	state := metadataState(t, repo, identity.UUID, "title")
	require.True(t, state.Protected)
	require.Equal(t, "unattributed", state.Origin)
	_, err := applyMetadata(repo, metadataInput(state, "inherit", "policy", `"Automatic replacement"`), true)
	require.ErrorIs(t, err, models.ErrMetadataFieldProtected)
	state, err = applyMetadata(repo, metadataInput(state, "set", "review", `"New title"`), false)
	require.NoError(t, err)
	history := metadataHistory(t, repo, identity.UUID, "title")
	require.Len(t, history, 2)
	require.Equal(t, "preserved", history[0].Mode)
	require.Equal(t, `"Kept image"`, string(history[0].Value))
	adopted := uuid.NewString()
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, identity.UUID, adopted, state.Entity.Revision)
		return err
	}))
	require.Equal(t, state.Decision.UUID, metadataState(t, repo, adopted, "title").Decision.UUID)
	history = metadataHistory(t, repo, adopted, "title")
	for _, d := range history {
		require.Equal(t, adopted, d.EntityUUID)
	}
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		page, err := repo.MetadataField.History(ctx, adopted, "title", history[0].Sequence, 1)
		require.NoError(t, err)
		require.Equal(t, history[1:], page)
		_, err = repo.MetadataField.History(ctx, adopted, "title", -1, 1)
		require.Error(t, err)
		return nil
	}))
	attachmentSQL(t, db, "DELETE FROM images WHERE id=41")
	attachmentSQL(t, db, "INSERT INTO images(id, created_at, updated_at) VALUES (41, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
	replacement := archiveFind(t, repo, models.ArchiveImage, 41)
	require.NotEqual(t, adopted, replacement.UUID)
	require.False(t, metadataState(t, repo, replacement.UUID, "title").Protected)
	require.Equal(t, history, metadataHistory(t, repo, adopted, "title"))
}

func TestMetadataFieldMigrationPreservesUnknownAndEmptyValues(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-field-choices.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+11; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	_, err = raw.Exec(archiveIdentityFixture + `INSERT INTO galleries(id, title, created_at, updated_at) VALUES (111, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
UPDATE scenes SET title='', date='2024-01-01', date_precision=2 WHERE id=31;`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	repo := db.Repository()
	for kind, ids := range map[models.ArchiveEntityKind][]int{models.ArchiveScene: {31, 32}, models.ArchiveImage: {41}, models.ArchiveGallery: {111}} {
		for _, id := range ids {
			identity := archiveFind(t, repo, kind, id)
			for _, def := range models.MetadataFields(kind) {
				state := metadataState(t, repo, identity.UUID, def.Name)
				require.Equal(t, "legacy", state.Origin)
				require.True(t, state.Protected)
				require.Nil(t, state.Decision)
			}
		}
	}
	identity := archiveFind(t, repo, models.ArchiveScene, 31)
	state := metadataState(t, repo, identity.UUID, "title")
	_, err = applyMetadata(repo, metadataInput(state, "inherit", "filename", `"Must not fill preserved empty title"`), true)
	require.ErrorIs(t, err, models.ErrMetadataFieldProtected)
	attachmentSQL(t, db, "UPDATE scenes SET title='Reviewed new title' WHERE id=31")
	history := metadataHistory(t, repo, identity.UUID, "title")
	require.Len(t, history, 2)
	require.Equal(t, "legacy", history[0].Origin)
	require.Equal(t, `""`, string(history[0].Value))
	require.Equal(t, "library", history[1].Origin)
	require.Equal(t, `"2024"`, string(metadataState(t, repo, identity.UUID, "date").Value))
	raw = openRawDB(t, path)
	defer raw.Close()
	require.Equal(t, uint(4), queryUint(t, raw, "SELECT count(*) FROM metadata_field_baselines"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestMetadataFieldFailureCannotCommitPartialChoice(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	identity := archiveFind(t, repo, models.ArchiveScene, 31)
	before := metadataState(t, repo, identity.UUID, "title")
	attachmentSQL(t, db, `CREATE TRIGGER reject_metadata_review BEFORE INSERT ON metadata_field_decisions
WHEN NEW.origin='review' BEGIN SELECT RAISE(ABORT, 'late metadata failure'); END;`)
	input := metadataInput(before, "set", "review", `"Must roll back"`)
	_, err := applyMetadata(repo, input, false)
	require.ErrorContains(t, err, "late metadata failure")
	require.Equal(t, before, metadataState(t, repo, identity.UUID, "title"))
	require.Empty(t, metadataHistory(t, repo, identity.UUID, "title"))
	// Even a caller that accidentally ignores the failure cannot commit the
	// changed field or the lazily captured baseline.
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.MetadataField.Decide(ctx, input)
		require.ErrorContains(t, err, "late metadata failure")
		return nil
	})
	require.ErrorContains(t, err, "unfinished metadata field write context")
	require.Equal(t, before, metadataState(t, repo, identity.UUID, "title"))
	attachmentSQL(t, db, "DROP TRIGGER reject_metadata_review")
	_, err = applyMetadata(repo, input, false)
	require.NoError(t, err)
}
