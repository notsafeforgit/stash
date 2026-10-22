package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestMediaRootIdentityMigrationPreservesLegacyBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prior.sqlite")
	replayBootstrapHistory(t, path, 1000127)
	raw := openRawDB(t, path)
	defer raw.Close()
	id := uuid.NewString()
	_, err := raw.Exec(`BEGIN;
 INSERT INTO media_roots(uuid) VALUES(?);
 INSERT INTO media_root_revisions(root_uuid,revision,label,state,server_path,directory_identity,origin,reason)
 VALUES(?,1,'Offline legacy mount','active','/missing/mount','2c:780','review','Existing binding');
 COMMIT;`, id, id)
	require.NoError(t, err)
	before := albumJobRows(t, raw, "media_root_revisions")
	db := sqlite.NewDatabase()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, db.Open(path), &needed)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	require.Equal(t, before, albumJobRows(t, raw, "media_root_revisions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM media_root_identity_upgrades"), "migration cannot assert filesystem equivalence")
	repo := db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		root, err := repo.MediaRoot.Find(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "2c:780", root.Binding.DirectoryIdentity)
		require.Empty(t, root.Binding.PersistentIdentity)
		require.Equal(t, 1, root.Revision)
		return err
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
	_, err = raw.Exec("DROP TRIGGER media_root_identity_upgrade_immutable")
	require.NoError(t, err)
	require.ErrorContains(t, db.Open(path), "media_root_identity_upgrade_immutable")
}
