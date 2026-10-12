package manager

import (
	"context"
	"encoding/json"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
	"testing"
)

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
