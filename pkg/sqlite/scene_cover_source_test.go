package sqlite_test

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSceneCoverSourcePersistenceAndUpstreamEdits(t *testing.T) {
	config.InitializeEmpty()
	database := sqlite.NewDatabase()
	database.SetBlobStoreOptions(sqlite.BlobStoreOptions{UseDatabase: true})
	path := filepath.Join(t.TempDir(), "covers.sqlite")
	require.NoError(t, database.Open(path))
	t.Cleanup(func() { _ = database.Close() })
	r := database.Repository()
	ctx := context.Background()
	scene := models.NewScene()
	cover := []byte("selected cover")
	source := models.SceneCoverSource{CoverChecksum: md5.FromBytes(cover), FileID: 42, At: 0,
		Fingerprint: models.SceneCoverFingerprint{Version: 1, Size: 1000, ModTimeNano: 123456789, MD5: "video"}}
	require.NoError(t, r.WithTxn(ctx, func(ctx context.Context) error {
		require.NoError(t, r.Scene.Create(ctx, &scene, nil))
		require.NoError(t, r.Scene.UpdateCover(ctx, scene.ID, cover))
		// The historical source ID deliberately does not require a live file.
		return r.Scene.SetCoverSource(ctx, scene.ID, &source)
	}))
	readSource := func() *models.SceneCoverSource {
		var saved *models.SceneCoverSource
		require.NoError(t, r.WithReadTxn(ctx, func(ctx context.Context) error {
			var err error
			saved, err = r.Scene.GetCoverSource(ctx, scene.ID)
			return err
		}))
		return saved
	}
	require.Equal(t, &source, readSource())
	require.NoError(t, database.Close())
	require.NoError(t, database.Open(path))
	require.Equal(t, &source, readSource())
	require.Equal(t, database.AppSchemaVersion(), database.Version())

	// A failed provenance write must roll the cover back in the same txn.
	for _, at := range []float64{-1, math.NaN(), math.Inf(1)} {
		err := r.WithTxn(ctx, func(ctx context.Context) error {
			require.NoError(t, r.Scene.UpdateCover(ctx, scene.ID, []byte("replacement")))
			invalid := source
			invalid.At = at
			return r.Scene.SetCoverSource(ctx, scene.ID, &invalid)
		})
		require.Error(t, err)
		require.Equal(t, &source, readSource())
	}
	require.NoError(t, r.WithTxn(ctx, func(ctx context.Context) error {
		source.At = 12.3456789
		return r.Scene.SetCoverSource(ctx, scene.ID, &source)
	}))
	require.Equal(t, &source, readSource())

	// Ordinary cover writers clear provenance only if the artwork changed.
	require.NoError(t, r.WithTxn(ctx, func(ctx context.Context) error { return r.Scene.UpdateCover(ctx, scene.ID, cover) }))
	require.Equal(t, &source, readSource())
	require.NoError(t, r.WithTxn(ctx, func(ctx context.Context) error { return r.Scene.UpdateCover(ctx, scene.ID, []byte("upload")) }))
	require.Nil(t, readSource())
	require.Error(t, r.WithTxn(ctx, func(ctx context.Context) error { return r.Scene.SetCoverSource(ctx, scene.ID, &source) }))

	require.NoError(t, r.WithTxn(ctx, func(ctx context.Context) error {
		require.NoError(t, r.Scene.UpdateCover(ctx, scene.ID, cover))
		return r.Scene.SetCoverSource(ctx, scene.ID, &source)
	}))
	// Upstream does not know about the sidecar. Its cover edit must still
	// invalidate reads immediately, and be reconciled on the next v3 open.
	raw := openRawDB(t, path)
	_, err := raw.Exec("UPDATE scenes SET cover_blob = NULL WHERE id = ?", scene.ID)
	require.NoError(t, err)
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM fork_scene_cover_sources"))
	require.NoError(t, raw.Close())
	require.Nil(t, readSource())
	require.NoError(t, database.Close())
	require.NoError(t, database.Open(path))
	require.Nil(t, readSource())
	raw = openRawDB(t, path)
	require.Equal(t, uint(0), queryUint(t, raw, "SELECT count(*) FROM fork_scene_cover_sources"))
	require.Equal(t, database.AppSchemaVersion(), queryUint(t, raw, "SELECT version FROM schema_migrations"))
	require.NoError(t, raw.Close())

	// Deleting a scene cascades its origin; recreating fork tables after an
	// upstream rebuild remains idempotent without changing the base schema.
	require.NoError(t, r.WithTxn(ctx, func(ctx context.Context) error {
		require.NoError(t, r.Scene.UpdateCover(ctx, scene.ID, cover))
		require.NoError(t, r.Scene.SetCoverSource(ctx, scene.ID, &source))
		return r.Scene.Destroy(ctx, scene.ID)
	}))
	require.Nil(t, readSource())
	raw = openRawDB(t, path)
	require.Equal(t, uint(0), queryUint(t, raw, "SELECT count(*) FROM fork_scene_cover_sources"))
	_, err = raw.Exec("DROP TABLE fork_scene_cover_sources")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, database.Close())
	require.NoError(t, database.Open(path))
	require.Nil(t, readSource())
}
