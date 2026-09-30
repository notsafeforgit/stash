package manager

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestConfigureDefaultFilterPreservesOtherViewsAndSettings(t *testing.T) {
	ctx := context.Background()
	m := defaultFilterTestManager(t, "")
	require.NoError(t, m.PromoteDefaultFilterConfig(ctx))
	ast, err := models.FilterASTFromLegacySavedFilter(map[string]interface{}{"favorite": map[string]interface{}{"value": true}})
	require.NoError(t, err)
	filter := &models.SavedFilter{Mode: models.FilterModeScenes, FilterAST: ast}
	_, err = m.ConfigureDefaultFilter(ctx, "scenes", "SET", filter, nil)
	require.NoError(t, err)
	ui, err := m.ConfigureDefaultFilter(ctx, "performer_scenes", "SET", filter, nil)
	require.NoError(t, err)
	require.Equal(t, "dark", ui["theme"])
	defaults := ui["defaultFilters"].(map[string]interface{})
	require.Len(t, defaults, 2)
	require.NotContains(t, defaults["scenes"], "object_filter")
	require.NotContains(t, ui, forkDefaultFilterStateKey)
	require.Equal(t, map[string]interface{}{"theme": "dark"}, m.Config.GetUIConfiguration())
	ui, err = m.ConfigureDefaultFilter(ctx, "scenes", "CLEAR", nil, nil)
	require.NoError(t, err)
	require.NotContains(t, ui["defaultFilters"], "scenes")
	require.Contains(t, ui["defaultFilters"], "performer_scenes")
	ui, err = m.ConfigureDefaultFilter(ctx, "performer_scenes", "CLEAR", nil, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{"theme": "dark"}, ui)
}

func TestConfigureDefaultFilterRejectsInvalidWrites(t *testing.T) {
	ctx := context.Background()
	m := defaultFilterTestManager(t, "")
	require.NoError(t, m.PromoteDefaultFilterConfig(ctx))
	_, err := m.ConfigureDefaultFilter(ctx, "scenes", "SET", &models.SavedFilter{Mode: models.FilterModeScenes}, nil)
	require.NoError(t, err)
	before, err := m.UIConfiguration(ctx)
	require.NoError(t, err)
	_, err = m.ConfigureDefaultFilter(ctx, "scenes.other", "CLEAR", nil, nil)
	require.Error(t, err)
	_, err = m.ConfigureDefaultFilter(ctx, "scenes", "SET", nil, nil)
	require.Error(t, err)
	_, err = m.ConfigureDefaultFilter(ctx, "scenes", "SET", &models.SavedFilter{Mode: models.FilterModeScenes, FilterAST: &models.FilterAST{}}, nil)
	require.Error(t, err)
	after, err := m.UIConfiguration(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestNativeDefaultFilterConflictResolutionChecksRevision(t *testing.T) {
	ctx := context.Background()
	for _, action := range []string{"USE_IMPORTED", "KEEP_CURRENT"} {
		t.Run(action, func(t *testing.T) {
			m := defaultFilterTestManager(t, importedConflictingDefault)
			require.NoError(t, m.PromoteDefaultFilterConfig(ctx))
			_, err := m.ConfigureDefaultFilter(ctx, "scenes", action, nil, nil)
			require.ErrorContains(t, err, "reviewed revision")
			stale := 0
			_, err = m.ConfigureDefaultFilter(ctx, "scenes", action, nil, &stale)
			require.ErrorContains(t, err, "changed")
			reviewed := 1
			ui, err := m.ConfigureDefaultFilter(ctx, "scenes", action, nil, &reviewed)
			require.NoError(t, err)
			require.NotContains(t, ui, "defaultFilterConflicts")
			require.NoError(t, m.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
				filter, err := m.Repository.DefaultFilter.Find(ctx, "scenes")
				require.NoError(t, err)
				require.Equal(t, 2, filter.Revision)
				if action == "USE_IMPORTED" {
					projection, valid := filter.Filter.FilterAST.FlatObjectFilter()
					require.True(t, valid)
					require.Equal(t, map[string]interface{}{"organized": true}, projection)
				} else {
					require.Equal(t, models.FilterGroupOperatorOr, filter.Filter.FilterAST.Root.Group.Operator)
				}
				return nil
			}))
			_, err = m.ConfigureDefaultFilter(ctx, "scenes", action, nil, &reviewed)
			require.ErrorContains(t, err, "changed")
		})
	}
}
