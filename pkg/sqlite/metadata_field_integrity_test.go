package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestMetadataFieldIntegrityAndIndexedHistory(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	identity := archiveFind(t, repo, models.ArchiveScene, 31)
	before := metadataState(t, repo, identity.UUID, "title")
	current, err := applyMetadata(repo, metadataInput(before, "set", "review", `"Chosen title"`), false)
	require.NoError(t, err)
	history := metadataHistory(t, repo, identity.UUID, "title")
	image := archiveFind(t, repo, models.ArchiveImage, 41)
	_, err = applyMetadata(repo, metadataInput(metadataState(t, repo, image.UUID, "title"), "set", "review", `"Other image title"`), false)
	require.NoError(t, err)
	other := metadataState(t, repo, image.UUID, "title")
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, tc := range []struct {
		query string
		args  []interface{}
	}{
		{"UPDATE metadata_field_decisions SET value_json='\"Rewritten evidence\"' WHERE uuid=?", []interface{}{current.Decision.UUID}},
		{"UPDATE metadata_field_decisions SET entity_uuid=? WHERE uuid=?", []interface{}{image.UUID, current.Decision.UUID}},
		{"UPDATE metadata_field_heads SET decision_uuid=? WHERE entity_uuid=? AND field='title'", []interface{}{history[0].UUID, identity.UUID}},
		{"UPDATE metadata_field_heads SET decision_uuid=? WHERE entity_uuid=? AND field='title'", []interface{}{other.Decision.UUID, identity.UUID}},
		{"UPDATE metadata_field_heads SET field='code' WHERE entity_uuid=? AND field='title'", []interface{}{identity.UUID}},
		{"INSERT INTO metadata_field_decisions(entity_uuid,field,mode,origin,value_json) VALUES (?,'filename','set','review','\"Bad target\"')", []interface{}{identity.UUID}},
		{"INSERT INTO metadata_field_decisions(entity_uuid,field,mode,origin,value_json) VALUES (?,'photographer','set','review','\"Wrong kind\"')", []interface{}{identity.UUID}},
		{"INSERT INTO metadata_field_decisions(entity_uuid,field,mode,origin,value_json) VALUES (?,'title','inherit','source','\"Missing capture\"')", []interface{}{identity.UUID}},
		{"INSERT INTO metadata_field_decisions(entity_uuid,field,mode,origin,value_json,capture_uuid) VALUES (?,'title','inherit','source','\"Unknown capture\"',?)", []interface{}{identity.UUID, uuid.NewString()}},
		{"INSERT INTO metadata_field_decisions(entity_uuid,field,mode,origin,value_json) VALUES (?,'title','clear','review','\"Not empty\"')", []interface{}{identity.UUID}},
		{"INSERT INTO metadata_field_decisions(entity_uuid,field,mode,origin,value_json) VALUES (?,'title','set','review','{}')", []interface{}{identity.UUID}},
	} {
		_, err := raw.Exec(tc.query, tc.args...)
		require.Error(t, err, tc.query)
	}
	var a, b, c int
	var plan string
	require.NoError(t, raw.QueryRow("EXPLAIN QUERY PLAN SELECT * FROM metadata_field_decisions WHERE entity_uuid=? AND field='title' AND sequence>0 ORDER BY sequence LIMIT 100", identity.UUID).Scan(&a, &b, &c, &plan))
	require.Contains(t, plan, "USING INDEX metadata_field_decisions_entity")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.Equal(t, current, metadataState(t, repo, identity.UUID, "title"))
}

func TestMetadataFieldMissingSourceAndUnfinishedContext(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	identity := archiveFind(t, repo, models.ArchiveScene, 31)
	state, err := applyMetadata(repo, metadataInput(metadataState(t, repo, identity.UUID, "title"), "inherit", "review", ""), false)
	require.NoError(t, err)
	input := metadataInput(state, "inherit", "source", `"Requires evidence"`)
	_, err = applyMetadata(repo, input, true)
	require.ErrorContains(t, err, "capture provenance")
	input.CaptureUUID = uuid.NewString()
	_, err = applyMetadata(repo, input, true)
	require.ErrorContains(t, err, "active source capture")
	input.Origin, input.CaptureUUID, input.Field = "filename", "", "details"
	_, err = applyMetadata(repo, input, true)
	require.ErrorContains(t, err, "only title")
	require.Equal(t, state, metadataState(t, repo, identity.UUID, "title"))
	attachmentSQL(t, db, "INSERT INTO metadata_field_write_context(entity_uuid,field) VALUES (?,'title')", identity.UUID)
	require.NoError(t, db.Close())
	require.ErrorContains(t, db.Open(db.DatabasePath()), "unfinished metadata field write context")
	raw := openRawDB(t, db.DatabasePath())
	_, err = raw.Exec("DELETE FROM metadata_field_write_context")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.Equal(t, state, metadataState(t, repo, identity.UUID, "title"))
}

func TestMetadataFieldAnonymisationRemovesPriorValuesAndSourceReferences(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	identity := archiveFind(t, repo, models.ArchiveScene, 31)
	state, err := applyMetadata(repo, metadataInput(metadataState(t, repo, identity.UUID, "title"), "inherit", "review", ""), false)
	require.NoError(t, err)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "private-metadata-field-post"}, "")
	capture := recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 1, "private-metadata-field-profile"))
	input := metadataInput(state, "inherit", "source", `"private-metadata-field-caption"`)
	input.CaptureUUID, input.Reason = capture.UUID, "private-metadata-field-reason"
	state, err = applyMetadata(repo, input, true)
	require.NoError(t, err)
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(context.Background()))
	contents, err := os.ReadFile(output)
	require.NoError(t, err)
	require.NotContains(t, string(contents), "private-metadata-field")
	require.NotContains(t, string(contents), state.Decision.UUID)
	raw := openRawDB(t, output)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_field_decisions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_field_heads"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.Equal(t, state, metadataState(t, repo, identity.UUID, "title"))
}
