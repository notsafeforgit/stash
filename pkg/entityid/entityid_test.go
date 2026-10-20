package entityid_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
	"github.com/stretchr/testify/require"
)

func TestIdentityCaptureUsesExistingWriteSnapshot(t *testing.T) {
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "identities.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	ctx := entityid.WithRepository(t.Context(), repo)
	var publicID string
	var scene models.Scene
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		scene = models.NewScene()
		require.NoError(t, repo.Scene.Create(ctx, &scene, nil))
		var err error
		publicID, err = entityid.Public(ctx, models.ArchiveScene, scene.ID)
		require.NoError(t, err, "uncommitted identity must use this writer snapshot")
		localID, err := entityid.Local(ctx, models.ArchiveScene, publicID)
		require.NoError(t, err)
		require.Equal(t, scene.ID, localID)
		return nil
	}))
	got, err := entityid.Public(ctx, models.ArchiveScene, scene.ID)
	require.NoError(t, err)
	require.Equal(t, publicID, got, "ordinary callers still get a managed read transaction")
}
