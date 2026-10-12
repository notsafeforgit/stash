package sqlite_test

import (
	"context"
	"errors"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestLibraryUUIDMigrationPreservesKeysAndUserReferences(t *testing.T) {
	config.InitializeEmpty()
	filename := filepath.Join(t.TempDir(), "before-uuids.sqlite")
	buildLegacyDatabase(t, filename, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(filename), &needed))
	migrator, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := migrator.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+114; version = migrator.CurrentSchemaVersion() {
		require.NoError(t, migrator.RunMigration(t.Context(), migrator.GetNextMigrationVersion(version)))
	}
	migrator.Close()
	raw := openRawDB(t, filename)
	_, err = raw.Exec(archiveIdentityFixture + `
 INSERT INTO tags(id,name,created_at,updated_at) VALUES(51,'Tag',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO galleries(id,title,created_at,updated_at) VALUES(61,'Gallery',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO scene_markers(id,title,seconds,scene_id,primary_tag_id,created_at,updated_at) VALUES(81,'Marker',1,31,51,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO galleries_chapters(id,title,image_index,gallery_id,created_at,updated_at) VALUES(91,'Chapter',0,61,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO saved_filters(id,mode,name,filter_ast) VALUES(101,'SCENES','Saved selection','{"root":{"condition":{"field":"performers","value":{"modifier":"INCLUDES","value":["71"],"excludes":["999999"]}}}}');
 INSERT INTO default_filters(view,mode,filter_ast) SELECT 'scenes',mode,filter_ast FROM saved_filters WHERE id=101;
 DELETE FROM metadata_field_pending;
 INSERT INTO shares(id,token_hash,label,created_by,created_at,expires_at,snapshot) VALUES('share',X'01','Frozen','owner',1,2,'{"entries":[{"kind":"SCENE","id":31,"title":"Kept title","media_keys":["scene-31"],"extra":"kept"}],"media":[],"extra":9007199254740993}');
 `)
	require.NoError(t, err)
	var oldScene string
	require.NoError(t, raw.QueryRow("SELECT uuid FROM archive_entities WHERE scene_id=31").Scan(&oldScene))
	var originalFilter string
	require.NoError(t, raw.QueryRow("SELECT filter_ast FROM saved_filters WHERE id=101").Scan(&originalFilter))
	_, err = raw.Exec(`UPDATE saved_filters SET filter_ast=json_set(filter_ast,'$.root.condition.value.value[0]','invalid-key') WHERE id=101`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	// A failure in the data conversion must roll back the schema rebuild, its
	// triggers, generated identities and version marker as one unit.
	require.ErrorContains(t, db.RunAllMigrations(), "invalid stored performer reference")
	raw = openRawDB(t, filename)
	require.EqualValues(t, 1000114, queryUint(t, raw, "SELECT version FROM schema_migrations"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_table_info('archive_entities') WHERE name='folder_id'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='native_library_identity_triggers'"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='metadata_reference_retiring'"))
	_, err = raw.Exec("UPDATE saved_filters SET filter_ast=? WHERE id=101", originalFilter)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	require.Equal(t, oldScene, archiveFind(t, repo, models.ArchiveScene, 31).UUID)
	for kind, id := range map[models.ArchiveEntityKind]int{models.ArchiveFolder: 1, models.ArchiveSceneMarker: 81, models.ArchiveGalleryChapter: 91, models.ArchiveSavedFilter: 101} {
		entity := archiveFind(t, repo, kind, id)
		require.Equal(t, id, *entity.LocalID)
		require.Equal(t, models.ArchiveEntityActive, entity.State)
	}
	performer := archiveFind(t, repo, models.ArchivePerformer, 71)
	raw = openRawDB(t, filename)
	defer raw.Close()
	var filterID, missing, defaultID, shareID, capability string
	require.NoError(t, raw.QueryRow(`SELECT json_extract(filter_ast,'$.root.condition.value.value[0]'),json_extract(filter_ast,'$.root.condition.value.excludes[0]') FROM saved_filters WHERE id=101`).Scan(&filterID, &missing))
	require.Equal(t, performer.UUID, filterID)
	require.Len(t, missing, 36)
	require.NoError(t, raw.QueryRow(`SELECT json_extract(filter_ast,'$.root.condition.value.value[0]') FROM default_filters WHERE view='scenes'`).Scan(&defaultID))
	require.Equal(t, performer.UUID, defaultID)
	require.NoError(t, raw.QueryRow(`SELECT json_extract(snapshot,'$.entries[0].uuid'),json_extract(snapshot,'$.entries[0].media_keys[0]') FROM shares WHERE id='share'`).Scan(&shareID, &capability))
	require.Equal(t, oldScene, shareID)
	require.Equal(t, "scene-31", capability)
	var sharedExtra string
	require.NoError(t, raw.QueryRow(`SELECT json_extract(snapshot,'$.entries[0].extra') || ':' || json_extract(snapshot,'$.extra') FROM shares WHERE id='share'`).Scan(&sharedExtra))
	require.Equal(t, "kept:9007199254740993", sharedExtra)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.SavedFilter.Destroy(ctx, 101) }))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(filename))
	var definition string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='archive_entity_redirect_update'").Scan(&definition))
	require.Contains(t, definition, "media_conversions")
}
