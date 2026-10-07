package manager

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin"
	"github.com/stashapp/stash/pkg/session"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type albumHookConfig struct {
	*config.Config
	directory string
}

func (c albumHookConfig) GetPluginsPath() string { return c.directory }

func TestAlbumBackfillHooksUseStableIdentityAndIgnoreDeletedLocalID(t *testing.T) {
	cfg := config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "albums.sqlite")))
	defer db.Close()
	repo := db.Repository()
	var entity *models.ArchiveEntity
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, "INSERT INTO galleries(id,title,created_at,updated_at) VALUES(7,'Album',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)", nil)
		if err != nil {
			return err
		}
		entity, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveGallery, 7)
		return err
	}))
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fixture.yml"), []byte(`apiVersion: 3
name: Album hook fixture
interface: js
exec: [hook.js]
hooks:
  - name: Created
    triggeredBy: [Gallery.Create.Post]
  - name: Updated
    triggeredBy: [Gallery.Update.Post]
`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hook.js"), []byte(`({Error: JSON.stringify(input.Args.hookContext)})`), 0600))
	cache := plugin.NewCache(albumHookConfig{Config: cfg, directory: dir})
	cache.RegisterSessionStore(session.NewStore(cfg))
	cache.ReloadPlugins()
	require.Len(t, cache.ListPlugins(), 1)
	mgr := &Manager{Repository: repo, PluginCache: cache}
	service := gallery.NewAlbumBackfill(repo)
	worker := mgr.NewAlbumBackfillWorker(service)
	require.NotNil(t, worker, "metadata-only albums need no FFmpeg configuration")
	published := gallery.AlbumPublication{EventUUID: uuid.NewString(), PostUUID: uuid.NewString(), GalleryUUID: entity.UUID, Created: true, Action: "create", Added: 1}
	guard := func(context.Context) error { return nil }
	err := worker.Effects(t.Context(), published, guard)
	require.Error(t, err)
	first := err.Error()
	// The actual JS plugin sees the same event identity on at-least-once replay.
	require.EqualError(t, worker.Effects(t.Context(), published, guard), first)
	start := strings.Index(first, "{\"eventId\"")
	require.NotEqual(t, -1, start, first)
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(first[start:]), &data))
	require.Equal(t, float64(7), data["id"])
	require.Equal(t, "Gallery.Create.Post", data["type"])
	require.Empty(t, data["inputFields"])
	mergeWorker := mgr.NewPostMergeNotificationWorker(gallery.NewPostMergeNotifications(repo))
	merge := models.PostConsolidationReview{Request: models.PostConsolidationReviewApplyInput{RequestUUID: published.EventUUID},
		Result: models.PostConsolidationReviewResult{Gallery: models.PostConsolidationGalleryResult{
			GalleryUUID: entity.UUID, Created: true, Action: "create", Added: []string{uuid.NewString()},
		}}}
	require.EqualError(t, mergeWorker.Effects(t.Context(), merge, guard), first)
	require.EqualError(t, mergeWorker.Effects(t.Context(), merge, guard), first, "merge retry retains the original hook event")
	published.Created, published.Action = false, "sync"
	updated := worker.Effects(t.Context(), published, guard)
	require.ErrorContains(t, updated, `"inputFields":["image_ids","scene_ids"]`)
	require.ErrorContains(t, updated, `"type":"Gallery.Update.Post"`)
	require.NotContains(t, updated.Error(), data["eventId"])
	merge.Result.Gallery.Created, merge.Result.Gallery.Action = false, "sync"
	require.EqualError(t, mergeWorker.Effects(t.Context(), merge, guard), updated.Error())
	published.Added = 0
	require.NoError(t, worker.Effects(t.Context(), published, guard), "a no-op album does not notify plugins")
	merge.Result.Gallery.Added = nil
	require.NoError(t, mergeWorker.Effects(t.Context(), merge, guard), "a no-op merge does not notify plugins")
	merge.Result.Gallery.Removed = []string{uuid.NewString()}
	require.EqualError(t, mergeWorker.Effects(t.Context(), merge, guard), updated.Error())
	published.Added = 1
	require.ErrorIs(t, worker.Effects(t.Context(), published, func(context.Context) error { return models.ErrArchiveJobLease }), models.ErrArchiveJobLease)
	require.ErrorIs(t, mergeWorker.Effects(t.Context(), merge, func(context.Context) error { return models.ErrArchiveJobLease }), models.ErrArchiveJobLease)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, "DELETE FROM galleries WHERE id=7; INSERT INTO galleries(id,title,created_at,updated_at) VALUES(7,'Replacement',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)", nil)
		return err
	}))
	require.NoError(t, worker.Effects(t.Context(), published, guard), "a deleted UUID cannot send an old event to its replacement local ID")
	require.NoError(t, mergeWorker.Effects(t.Context(), merge, guard), "a deleted UUID cannot send merge notifications to its replacement local ID")
}
