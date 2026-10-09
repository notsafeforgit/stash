package sqlite_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestRetiredSourceDefinitionsCanBeRestoredAfterMigration(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "retired.sqlite")
	buildLegacyDatabase(t, path, sqlite.NativeSchemaBaseline+101, false)
	raw := openRawDB(t, path)
	defer raw.Close()
	rootID, collectionID := uuid.NewString(), uuid.NewString()
	tx, err := raw.Begin()
	require.NoError(t, err)
	_, err = tx.Exec("INSERT INTO media_roots(uuid) VALUES(?)", rootID)
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO media_root_revisions(root_uuid,revision,label,state,origin,reason)
VALUES(?,1,'Retained root','retired','review','Retired before migration')`, rootID)
	require.NoError(t, err)
	_, err = tx.Exec("INSERT INTO source_collections(uuid) VALUES(?)", collectionID)
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO source_collection_revisions(collection_uuid,revision,label,kind,namespace,state,target_url,root_uuid,path_prefix,origin,reason)
VALUES(?,1,'Retained collection','directory','','retired','',?,'.','review','Retired before migration')`, collectionID, rootID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	_, err = raw.Exec(`INSERT INTO media_root_revisions(root_uuid,revision,label,state,origin,reason)
VALUES(?,2,'Retained root','active','review','Restore')`, rootID)
	require.ErrorContains(t, err, "retired")
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	repo := db.Repository()
	root := putMediaRoot(t, repo, models.MediaRootInput{UUID: rootID, ExpectedRevision: 1, Origin: "review",
		MediaRootDefinition: models.MediaRootDefinition{Label: "Retained root", State: "active"}})
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{UUID: collectionID, ExpectedRevision: 1, Origin: "review",
		SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Retained collection", Kind: "directory", State: "active", RootUUID: &rootID, PathPrefix: "."}})
	require.Equal(t, 2, root.Revision)
	require.Equal(t, 2, collection.Revision)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000102"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM media_root_revisions WHERE state='retired'"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_collection_revisions WHERE state='retired'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
