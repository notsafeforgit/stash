package manager

import (
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func defaultFilterTestManager(t *testing.T) *Manager {
	t.Helper()
	c := config.InitializeEmpty()
	c.SetConfigFile(filepath.Join(t.TempDir(), "config.yml"))
	c.SetUIConfiguration(map[string]interface{}{"theme": "dark"})
	return &Manager{Config: c}
}

func TestConfigureDefaultFilterPreservesOtherViewsAndSettings(t *testing.T) {
	m := defaultFilterTestManager(t)
	ast, err := models.FilterASTFromLegacySavedFilter(map[string]interface{}{"favorite": map[string]interface{}{"value": true}})
	require.NoError(t, err)
	filter := &models.SavedFilter{Mode: models.FilterModeScenes, FilterAST: ast}
	_, err = m.ConfigureDefaultFilter("scenes", "SET", filter)
	require.NoError(t, err)
	ui, err := m.ConfigureDefaultFilter("performer_scenes", "SET", filter)
	require.NoError(t, err)
	require.Equal(t, "dark", ui["theme"])
	defaults := ui["defaultFilters"].(map[string]interface{})
	require.Len(t, defaults, 2)
	projection, _ := ast.FlatObjectFilter()
	require.Equal(t, projection, defaults["scenes"].(map[string]interface{})["object_filter"])
	ui, err = m.ConfigureDefaultFilter("scenes", "CLEAR", nil)
	require.NoError(t, err)
	require.NotContains(t, ui["defaultFilters"], "scenes")
	require.Contains(t, ui["defaultFilters"], "performer_scenes")
	require.NotContains(t, ui[forkDefaultFilterStateKey], "scenes")
	ui, err = m.ConfigureDefaultFilter("performer_scenes", "CLEAR", nil)
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{"theme": "dark"}, ui)
}

func TestConfigureDefaultFilterEmptyAndInvalidFilters(t *testing.T) {
	m := defaultFilterTestManager(t)
	_, err := m.ConfigureDefaultFilter("scenes", "SET", &models.SavedFilter{Mode: models.FilterModeScenes})
	require.NoError(t, err)
	before := m.Config.GetUIConfiguration()
	_, err = m.ConfigureDefaultFilter("scenes.other", "CLEAR", nil)
	require.Error(t, err)
	_, err = m.ConfigureDefaultFilter("scenes", "SET", nil)
	require.Error(t, err)
	_, err = m.ConfigureDefaultFilter("scenes", "SET", &models.SavedFilter{Mode: models.FilterModeScenes, FilterAST: &models.FilterAST{}})
	require.Error(t, err)
	require.Equal(t, before, m.Config.GetUIConfiguration())
}

func TestDefaultFilterRollbackAndRollForward(t *testing.T) {
	for _, action := range []string{"USE_LEGACY", "KEEP_V3"} {
		t.Run(action, func(t *testing.T) {
			m := defaultFilterTestManager(t)
			ast, err := decodeDefaultFilterAST(map[string]interface{}{"root": map[string]interface{}{"group": map[string]interface{}{
				"operator": "OR", "children": []interface{}{
					map[string]interface{}{"condition": map[string]interface{}{"field": "favorite", "value": map[string]interface{}{"value": true}}},
					map[string]interface{}{"condition": map[string]interface{}{"field": "rating100", "value": map[string]interface{}{"value": 80, "modifier": "GREATER_THAN"}}},
				},
			}}})
			require.NoError(t, err)
			_, err = m.ConfigureDefaultFilter("scenes", "SET", &models.SavedFilter{Mode: models.FilterModeScenes, FilterAST: ast})
			require.NoError(t, err)
			// v2.5 replaces the default with only fields it understands while
			// leaving the fork-owned namespace untouched.
			legacy := map[string]interface{}{"organized": true}
			_, err = m.Config.UpdateUIConfiguration(func(ui map[string]interface{}) (map[string]interface{}, error) {
				ui["defaultFilters"].(map[string]interface{})["scenes"] = map[string]interface{}{
					"mode": "SCENES", "object_filter": legacy, "ui_options": map[string]interface{}{"display_mode": 2},
				}
				return ui, nil
			})
			require.NoError(t, err)
			// Resolution must reconcile this latest write even without a restart.
			ui, err := m.ConfigureDefaultFilter("scenes", action, nil)
			require.NoError(t, err)
			entry := ui["defaultFilters"].(map[string]interface{})["scenes"].(map[string]interface{})
			state := ui[forkDefaultFilterStateKey].(map[string]interface{})["scenes"].(map[string]interface{})
			require.NotContains(t, state, "pending_legacy_object_filter")
			require.Equal(t, entry["object_filter"], state["legacy_object_filter"])
			require.NotEmpty(t, entry["ui_options"])
			if action == "USE_LEGACY" {
				require.Equal(t, legacy, entry["object_filter"])
			} else {
				got, err := decodeDefaultFilterAST(entry["filter_ast"])
				require.NoError(t, err)
				require.Equal(t, ast, got)
			}
			// Reopening v3 must not rediscover an already resolved conflict.
			_, err = reconcileDefaultFilterConfig(ui)
			require.NoError(t, err)
			require.NotContains(t, state, "pending_legacy_object_filter")
		})
	}
}
