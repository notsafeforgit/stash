package manager

import (
	"context"
	"encoding/json"
	"github.com/stashapp/stash/internal/identify"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskUUIDConfigurationPromotionAndValidation(t *testing.T) {
	m := defaultFilterTestManager(t, `{"taskDefaults":{"autoTag":{"performers":["*"],"tags":["23"]},"generate":{"sceneIDs":["19"]}}}`)
	require.NoError(t, m.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := m.Database.ExecSQL(ctx, `INSERT INTO tags(id,name,created_at,updated_at) VALUES(23,'Review',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO scenes(id,created_at,updated_at) VALUES(19,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, nil)
		return err
	}))
	oldTag, missingTag := "23", "99999"
	m.Config.SetInterface(config.DefaultIdentifySettings, &identify.Options{
		SceneIDs: []string{"19"},
		Options:  &identify.MetadataOptions{SkipMultipleMatchTag: &oldTag},
		Sources:  []*identify.Source{{Options: &identify.MetadataOptions{SkipSingleNamePerformerTag: &missingTag}}},
	})
	m.Config.SetInterface(config.DefaultAutoTagSettings, &config.AutoTagMetadataOptions{Tags: []string{oldTag}, Performers: []string{"*"}})
	require.NoError(t, m.Config.Write())
	path := m.Config.GetConfigFile()
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	m.Config.SetConfigFile(filepath.Join(t.TempDir(), "missing", "config.yml"))
	require.Error(t, m.PromoteLibraryUUIDConfig(t.Context()))
	require.Equal(t, oldTag, *m.Config.GetDefaultIdentifySettings().Options.SkipMultipleMatchTag)
	m.Config.SetConfigFile(path)
	unchanged, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, unchanged)
	require.NoError(t, m.PromoteLibraryUUIDConfig(t.Context()))
	ctx := entityid.WithRepository(t.Context(), m.Repository)
	tagUUID, err := entityid.Public(ctx, models.ArchiveTag, 23)
	require.NoError(t, err)
	sceneUUID, err := entityid.Public(ctx, models.ArchiveScene, 19)
	require.NoError(t, err)
	options := m.Config.GetDefaultIdentifySettings()
	require.Equal(t, tagUUID, *options.Options.SkipMultipleMatchTag)
	require.Equal(t, []string{sceneUUID}, options.SceneIDs)
	require.NotEqual(t, missingTag, *options.Sources[0].Options.SkipSingleNamePerformerTag)
	require.NoError(t, ValidateLibraryConfigReferences(ctx, m.Config.Snapshot()))
	require.Equal(t, []string{tagUUID}, m.Config.GetDefaultAutoTagSettings().Tags)
	require.Equal(t, []string{"*"}, m.Config.GetDefaultAutoTagSettings().Performers)
	promoted, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, m.PromoteLibraryUUIDConfig(t.Context()))
	again, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, promoted, again)
	for _, value := range []any{23, "23", sceneUUID} {
		require.Error(t, ValidateLibraryUIReferences(ctx, map[string]any{"taskDefaults": map[string]any{
			"autoTag": map[string]any{"tags": []any{value}},
		}}))
	}
}

func TestLibraryUUIDConfigurationMigration(t *testing.T) {
	m := defaultFilterTestManager(t, `{"theme":"dark","provider":{"id":"17"},"frontPageContent":[{"__typename":"SavedFilter","savedFilterId":17}],"tv":{"sceneFilter":{"kind":"saved","id":"17"},"rail":[{"type":"action","action":{"id":"17","kind":"quick-tag","tagId":"23"}},{"type":"folder","actions":[{"id":"23","kind":"quick-marker","primaryTagId":"23","tagIds":["23","99999"]}]}]}}`)
	require.NoError(t, m.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := m.Database.ExecSQL(ctx, `INSERT INTO saved_filters(id,mode,name) VALUES(17,'SCENES','Favorites'); INSERT INTO tags(id,name,created_at,updated_at) VALUES(23,'Tag',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, nil)
		return err
	}))
	require.NoError(t, m.PromoteLibraryUUIDConfig(t.Context()))
	current := m.Config.GetUIConfiguration()
	bytes, err := json.Marshal(current)
	require.NoError(t, err)
	require.Contains(t, string(bytes), `"provider":{"id":"17"}`)
	require.Contains(t, string(bytes), `"id":"17","kind":"quick-tag"`)
	ctx := entityid.WithRepository(t.Context(), m.Repository)
	require.NoError(t, ValidateLibraryUIReferences(ctx, current))
	savedID, err := entityid.Public(ctx, models.ArchiveSavedFilter, 17)
	require.NoError(t, err)
	tagID, err := entityid.Public(ctx, models.ArchiveTag, 23)
	require.NoError(t, err)
	require.Contains(t, string(bytes), savedID)
	require.Contains(t, string(bytes), tagID)
	require.NotContains(t, string(bytes), `"99999"`)
	require.NoError(t, m.PromoteLibraryUUIDConfig(t.Context()))
	again, err := json.Marshal(m.Config.GetUIConfiguration())
	require.NoError(t, err)
	require.Equal(t, bytes, again)
	require.Error(t, ValidateLibraryUIReferences(ctx, map[string]any{"frontPageContent": []any{map[string]any{"__typename": "SavedFilter", "savedFilterId": "17"}}}))
	require.Error(t, ValidateLibraryUIReferences(ctx, map[string]any{"frontPageContent": []any{map[string]any{"__typename": "SavedFilter", "savedFilterId": tagID}}}))
}
