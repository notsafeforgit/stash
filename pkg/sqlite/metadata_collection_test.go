package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func metadataReferenceInput(t *testing.T, state *models.MetadataFieldState, targets ...*models.ArchiveEntity) models.MetadataFieldDecisionInput {
	ids := make([]string, 0, len(targets))
	revisions := make(map[string]int)
	for _, target := range targets {
		ids = append(ids, target.UUID)
		revisions[target.UUID] = target.Revision
	}
	raw, err := json.Marshal(ids)
	require.NoError(t, err)
	input := metadataInput(state, "set", "review", string(raw))
	input.ReferenceRevisions = revisions
	return input
}

func TestMetadataCollectionCoalescesEditsAndPreservesEmptyIntent(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	before := metadataHistory(t, repo, scene.UUID, "performers")
	require.Len(t, before, 1)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `DELETE FROM performers_scenes WHERE scene_id=31;
INSERT INTO performers_scenes(performer_id,scene_id) VALUES(71,31),(72,31);
DELETE FROM performers_scenes WHERE scene_id=31 AND performer_id=71;`, nil)
		require.NoError(t, err)
		pending, err := repo.MetadataField.State(ctx, scene.UUID, "performers")
		require.NoError(t, err)
		require.True(t, pending.Pending)
		require.True(t, pending.Protected)
		history, err := repo.MetadataField.History(ctx, scene.UUID, "performers", 0, 100)
		require.NoError(t, err)
		require.Equal(t, before, history)
		return nil
	}))
	history := metadataHistory(t, repo, scene.UUID, "performers")
	require.Len(t, history, 2)
	p := archiveFind(t, repo, models.ArchivePerformer, 72)
	require.JSONEq(t, `["`+p.UUID+`"]`, string(history[1].Value))
	require.False(t, metadataState(t, repo, scene.UUID, "performers").Pending)
	for _, mode := range []models.RelationshipUpdateMode{models.RelationshipUpdateModeSet, models.RelationshipUpdateModeAdd, models.RelationshipUpdateModeRemove} {
		state, err := applyMetadata(repo, metadataInput(metadataState(t, repo, scene.UUID, "tags"), "inherit", "review", "[]"), false)
		require.NoError(t, err)
		ids := []int{}
		if mode != models.RelationshipUpdateModeSet {
			ids = []int{81}
		}
		require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
			_, err := repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{TagIDs: &models.UpdateIDs{IDs: ids, Mode: mode}})
			return err
		}))
		next := metadataState(t, repo, scene.UUID, "tags")
		require.True(t, next.Protected)
		require.Equal(t, "library", next.Origin)
		require.Greater(t, next.Entity.Revision, state.Entity.Revision)
	}
	// Empty URL/group updates have no rows for SQL triggers to observe.
	for _, field := range []string{"urls", "groups"} {
		_, err := applyMetadata(repo, metadataInput(metadataState(t, repo, scene.UUID, field), "inherit", "review", "[]"), false)
		require.NoError(t, err)
	}
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{
			URLs:     &models.UpdateStrings{Values: []string{}, Mode: models.RelationshipUpdateModeSet},
			GroupIDs: &models.UpdateGroupIDs{Groups: []models.GroupsScenes{}, Mode: models.RelationshipUpdateModeSet},
		})
		return err
	}))
	for _, field := range []string{"urls", "groups"} {
		require.Equal(t, "clear", metadataState(t, repo, scene.UUID, field).Mode)
	}
	// Existing image and custom-field APIs must preserve explicitly empty sets.
	image := archiveFind(t, repo, models.ArchiveImage, 41)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		if err := repo.Image.UpdatePerformers(ctx, 41, nil); err != nil {
			return err
		}
		return repo.Image.SetCustomFields(ctx, 41, models.CustomFieldsInput{Full: map[string]interface{}{}})
	}))
	for _, field := range []string{"performers", "custom_fields"} {
		require.Equal(t, "clear", metadataState(t, repo, image.UUID, field).Mode)
	}
	// Reaffirming an inherited existing relationship becomes a protected choice.
	state := metadataState(t, repo, scene.UUID, "performers")
	input := metadataReferenceInput(t, state, p)
	input.Mode = "inherit"
	_, err := applyMetadata(repo, input, false)
	require.NoError(t, err)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{PerformerIDs: &models.UpdateIDs{IDs: []int{72}, Mode: models.RelationshipUpdateModeAdd}})
		return err
	}))
	require.True(t, metadataState(t, repo, scene.UUID, "performers").Protected)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.Equal(t, "library", metadataState(t, repo, scene.UUID, "performers").Origin)
}

func TestMetadataCollectionTargetRevisionsAndIdentityHistory(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	state := metadataState(t, repo, scene.UUID, "performers")
	p := archiveFind(t, repo, models.ArchivePerformer, 71)
	input := metadataReferenceInput(t, state, p)
	_, err := applyMetadata(repo, input, false)
	require.NoError(t, err)
	state = metadataState(t, repo, scene.UUID, "performers")
	input = metadataReferenceInput(t, state, p)
	input.ReferenceRevisions[p.UUID]--
	_, err = applyMetadata(repo, input, false)
	require.ErrorIs(t, err, models.ErrMetadataFieldConflict)
	wrong := archiveFind(t, repo, models.ArchiveTag, 81)
	_, err = applyMetadata(repo, metadataReferenceInput(t, state, wrong), false)
	require.ErrorIs(t, err, models.ErrMetadataFieldConflict)
	missing := &models.ArchiveEntity{UUID: uuid.NewString(), Revision: 1}
	_, err = applyMetadata(repo, metadataReferenceInput(t, state, missing), false)
	require.ErrorIs(t, err, models.ErrMetadataFieldConflict)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	merged := archiveFind(t, repo, models.ArchivePerformer, 72)
	state = metadataState(t, repo, scene.UUID, "performers")
	require.JSONEq(t, `["`+merged.UUID+`"]`, string(state.Value))
	history := metadataHistory(t, repo, scene.UUID, "performers")
	require.JSONEq(t, `["`+p.UUID+`"]`, string(history[0].Value))
	require.Len(t, history, 3)
	_, err = applyMetadata(repo, metadataReferenceInput(t, state, p), false)
	require.ErrorIs(t, err, models.ErrMetadataFieldConflict)
	adopted := uuid.NewString()
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, merged.UUID, adopted, merged.Revision)
		return err
	}))
	require.JSONEq(t, `["`+adopted+`"]`, string(metadataState(t, repo, scene.UUID, "performers").Value))
	history = metadataHistory(t, repo, scene.UUID, "performers")
	require.JSONEq(t, `["`+adopted+`"]`, string(history[len(history)-1].Value))
	attachmentSQL(t, db, "DELETE FROM performers WHERE id=72")
	history = metadataHistory(t, repo, scene.UUID, "performers")
	require.JSONEq(t, `["`+adopted+`"]`, string(history[len(history)-2].Value))
	require.Equal(t, "[]", string(history[len(history)-1].Value))
	attachmentSQL(t, db, "INSERT INTO performers(id,created_at,updated_at) VALUES(72,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)")
	require.NotEqual(t, adopted, archiveFind(t, repo, models.ArchivePerformer, 72).UUID)
	require.Equal(t, "[]", string(metadataState(t, repo, scene.UUID, "performers").Value))
}

func TestMetadataCollectionValidationReplayAndRollback(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	for field, values := range map[string][]string{
		"urls":          {`null`, `["file:///tmp/private"]`, `["/relative"]`, `[false]`},
		"custom_fields": {`null`, `[]`, `{"nested":{}}`, `{"nil":null}`, `{"huge":9223372036854775808}`, `{" key":"bad"}`},
		"performers":    {`null`, `["bad"]`, `[31]`},
		"groups":        {`[{}]`, `[{"uuid":"bad","scene_index":2}]`},
	} {
		state := metadataState(t, repo, scene.UUID, field)
		for _, value := range values {
			_, err := applyMetadata(repo, metadataInput(state, "set", "review", value), false)
			require.Error(t, err, field+value)
		}
		require.Equal(t, state, metadataState(t, repo, scene.UUID, field))
	}
	state := metadataState(t, repo, scene.UUID, "custom_fields")
	state, err := applyMetadata(repo, metadataInput(state, "set", "review", `{"enabled":true,"number":0.3333333333333333,"large_float":1e20,"negative_limit":-9223372036854775808.0,"large_integer_double":3074457345618258432.0,"id":9223372036854775807}`), false)
	require.NoError(t, err)
	require.JSONEq(t, `{"enabled":1,"number":0.3333333333333333,"large_float":1e20,"negative_limit":-9223372036854775808.0,"large_integer_double":3074457345618258432.0,"id":9223372036854775807}`, string(state.Value))
	input := metadataInput(state, "set", "review", string(state.Value))
	replay, err := applyMetadata(repo, input, false)
	require.NoError(t, err)
	require.Equal(t, state, replay)
	input.Reason = "A new review of the same numeric values"
	reapplied, err := applyMetadata(repo, input, false)
	require.NoError(t, err)
	require.Equal(t, state.Value, reapplied.Value)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		values, err := repo.Scene.GetCustomFields(ctx, 31)
		require.NoError(t, err)
		require.Equal(t, map[string]interface{}{
			"enabled": int64(1), "number": 0.3333333333333333, "large_float": 1e20,
			"negative_limit": -9223372036854775808.0, "large_integer_double": 3074457345618258432.0,
			"id": int64(9223372036854775807),
		}, values)
		return nil
	}))
	state = metadataState(t, repo, scene.UUID, "performers")
	p := archiveFind(t, repo, models.ArchivePerformer, 72)
	input = metadataReferenceInput(t, state, p)
	attachmentSQL(t, db, `CREATE TRIGGER reject_reference_seal BEFORE UPDATE OF sealed ON metadata_field_decisions
WHEN NEW.field='performers' BEGIN SELECT RAISE(ABORT,'late reference failure'); END;`)
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.MetadataField.Decide(ctx, input)
		require.ErrorContains(t, err, "late reference failure")
		return nil
	})
	require.ErrorContains(t, err, "unfinished metadata field write context")
	require.Equal(t, state, metadataState(t, repo, scene.UUID, "performers"))
	attachmentSQL(t, db, "DROP TRIGGER reject_reference_seal")
	// A regular multi-row edit that cannot be recorded fails the commit too.
	attachmentSQL(t, db, `CREATE TRIGGER reject_collection_commit BEFORE INSERT ON metadata_field_decisions
WHEN NEW.field='urls' BEGIN SELECT RAISE(ABORT,'cannot record collection'); END;`)
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO scene_urls(scene_id,position,url) VALUES(31,0,'https://example.test/rollback')`, nil)
		return err
	})
	require.ErrorContains(t, err, "cannot record collection")
	require.Equal(t, "[]", string(metadataState(t, repo, scene.UUID, "urls").Value))
}

func TestMetadataCollectionStartupAndReferenceGuards(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	state := metadataState(t, repo, scene.UUID, "performers")
	p := archiveFind(t, repo, models.ArchivePerformer, 72)
	state, err := applyMetadata(repo, metadataReferenceInput(t, state, p), false)
	require.NoError(t, err)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, query := range []string{
		`UPDATE metadata_field_references SET position=2 WHERE decision_uuid=?`,
		`UPDATE metadata_field_references SET scene_index=1 WHERE decision_uuid=?`,
		`UPDATE metadata_field_decisions SET sealed=0 WHERE uuid=?`,
		`UPDATE metadata_field_decisions SET reference_count=0 WHERE uuid=?`,
	} {
		_, err := raw.Exec(query, state.Decision.UUID)
		require.Error(t, err, query)
	}
	rows, err := raw.Query(`EXPLAIN QUERY PLAN SELECT value_json FROM metadata_collection_values WHERE entity_uuid=? AND field='performers'`, scene.UUID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, rows.Scan(&a, &b, &c, &detail))
		require.NotContains(t, detail, "SCAN a")
		require.NotContains(t, detail, "SCAN j")
	}
	require.NoError(t, rows.Err())

	for _, fixture := range []string{
		fmt.Sprintf(`INSERT INTO metadata_field_pending(entity_uuid,field,previous_json) VALUES('%s','tags','[]')`, scene.UUID),
		fmt.Sprintf(`INSERT INTO metadata_field_decisions(entity_uuid,field,mode,origin,value_json,sealed) VALUES('%s','tags','set','review','null',0)`, scene.UUID),
	} {
		_, err = raw.Exec(fixture)
		require.NoError(t, err)
		require.NoError(t, db.Close())
		require.ErrorContains(t, db.Open(db.DatabasePath()), "unfinished metadata collection")
		_, err = raw.Exec("DELETE FROM metadata_field_pending; DELETE FROM metadata_field_decisions WHERE sealed=0")
		require.NoError(t, err)
		require.NoError(t, db.Open(db.DatabasePath()))
	}
	require.Equal(t, state, metadataState(t, repo, scene.UUID, "performers"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestMetadataCollectionMigrationAndPreviouslyUnrecordedDeletion(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-collections.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+12; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	_, err = raw.Exec(archiveIdentityFixture + archiveMetadataFixture + `UPDATE scenes SET title='Already reviewed',studio_id=91 WHERE id=31;
INSERT INTO groups_scenes(group_id,scene_id,scene_index) VALUES(101,31,4);
INSERT INTO scenes_tags(scene_id,tag_id) VALUES(31,81);`)
	require.NoError(t, err)
	snapshot := func() string {
		var ret string
		require.NoError(t, raw.QueryRow(`SELECT json_group_array(json_object('uuid',uuid,'entity',entity_uuid,'field',field,'mode',mode,'origin',origin,'value',value_json,'sequence',sequence,'created',created_at)) FROM (SELECT * FROM metadata_field_decisions ORDER BY sequence)`).Scan(&ret))
		return ret
	}
	before := snapshot()
	// Even removed tail rows must not allow history cursor values to be reused.
	_, err = raw.Exec("UPDATE sqlite_sequence SET seq=seq+100 WHERE name='metadata_field_decisions'")
	require.NoError(t, err)
	lastSequence := queryUint(t, raw, "SELECT seq FROM sqlite_sequence WHERE name='metadata_field_decisions'")
	var sceneUUID, performerUUID, studioUUID, groupUUID string
	var revision int
	require.NoError(t, raw.QueryRow("SELECT uuid,revision FROM archive_entities WHERE scene_id=31").Scan(&sceneUUID, &revision))
	for column, dest := range map[string]*string{"performer_id": &performerUUID, "studio_id": &studioUUID, "group_id": &groupUUID} {
		require.NoError(t, raw.QueryRow("SELECT uuid FROM archive_entities WHERE "+column+" IS NOT NULL ORDER BY "+column+" LIMIT 1").Scan(dest))
	}
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	require.Equal(t, before, snapshot())
	require.Equal(t, lastSequence, queryUint(t, raw, "SELECT seq FROM sqlite_sequence WHERE name='metadata_field_decisions'"))
	repo := db.Repository()
	require.Equal(t, revision, archiveFind(t, repo, models.ArchiveScene, 31).Revision)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_field_references"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_field_pending"))
	require.JSONEq(t, `"Already reviewed"`, string(metadataState(t, repo, sceneUUID, "title").Value))
	require.True(t, metadataState(t, repo, sceneUUID, "performers").Protected)
	// First deletion must retain the old UUID even though the FK is then nulled
	// and the native integer ID may later be reused.
	attachmentSQL(t, db, "DELETE FROM performers WHERE id=71; DELETE FROM studios WHERE id=91; DELETE FROM groups WHERE id=101;")
	for field, previous := range map[string]string{
		"performers": `["` + performerUUID + `"]`, "studio": `"` + studioUUID + `"`, "groups": `[{"uuid":"` + groupUUID + `","scene_index":4}]`,
	} {
		history := metadataHistory(t, repo, sceneUUID, field)
		require.Len(t, history, 2)
		require.Greater(t, history[0].Sequence, int(lastSequence))
		require.Equal(t, "preserved", history[0].Mode)
		require.JSONEq(t, previous, string(history[0].Value))
		require.Equal(t, "clear", history[1].Mode)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, raw.Close())
}
