package plugin

import (
	"context"
	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stashapp/stash/pkg/plugin/common"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stretchr/testify/require"
	"testing"
)

type pluginIdentityStore struct {
	models.ArchiveEntityReaderWriter
	rows map[string]*models.ArchiveEntity
}

func (s *pluginIdentityStore) Resolve(_ context.Context, id string) (*models.ArchiveEntity, error) {
	return s.rows[id], nil
}
func (s *pluginIdentityStore) FindByLocalID(_ context.Context, kind models.ArchiveEntityKind, id int) (*models.ArchiveEntity, error) {
	for _, row := range s.rows {
		if row.Kind == kind && row.State == models.ArchiveEntityActive && row.LocalID != nil && *row.LocalID == id {
			return row, nil
		}
	}
	return nil, nil
}
func pluginLibraryFixture(t *testing.T) (*pluginIdentityStore, models.Repository, string, string) {
	t.Helper()
	store := &pluginIdentityStore{rows: map[string]*models.ArchiveEntity{}}
	sceneID, imageID := uuid.NewString(), uuid.NewString()
	sceneKey, imageKey := 42, 7
	store.rows[sceneID] = &models.ArchiveEntity{UUID: sceneID, Kind: models.ArchiveScene, State: models.ArchiveEntityActive, LocalID: &sceneKey}
	store.rows[imageID] = &models.ArchiveEntity{UUID: imageID, Kind: models.ArchiveImage, State: models.ArchiveEntityActive, LocalID: &imageKey}
	repo := models.Repository{TxnManager: mocks.NewDatabase(), ArchiveEntity: store}
	return store, repo, sceneID, imageID
}
func TestPluginNotificationRetainsDeletedUUIDAndPreservesOpaqueFields(t *testing.T) {
	store, repo, sceneID, imageID := pluginLibraryFixture(t)
	c := NewCache(previewServerConfig{})
	c.RegisterRepository(repo)
	ctx := entityid.WithRepository(t.Context(), repo)
	entityid.Remember(ctx, models.ArchiveImage, 7, imageID)
	store.rows[imageID].State = models.ArchiveEntityDeleted
	replacement := uuid.NewString()
	key := 7
	store.rows[replacement] = &models.ArchiveEntity{UUID: replacement, Kind: models.ArchiveImage, State: models.ArchiveEntityActive, LocalID: &key}
	notice, err := c.publicHookContext(ctx, hook.ImageDestroyPost, common.HookContext{ID: 7, Input: map[string]any{
		"id": "7", "scene_ids": []string{"42"}, "custom_fields": map[string]any{"id": "123"}, "stash_ids": []map[string]any{{"stash_id": "42"}},
	}})
	require.NoError(t, err)
	require.Equal(t, imageID, notice.UUID)
	input := notice.Input.(map[string]any)
	require.Equal(t, imageID, input["id"])
	require.Equal(t, []any{sceneID}, input["scene_ids"])
	require.Equal(t, map[string]any{"id": "123"}, input["custom_fields"])
	actual, err := entityid.Public(entityid.WithRepository(t.Context(), repo), models.ArchiveImage, 7)
	require.NoError(t, err)
	require.Equal(t, replacement, actual)
}
