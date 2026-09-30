package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultFilterPublicationSurvivesConfigReload(t *testing.T) {
	c := InitializeEmpty()
	path := filepath.Join(t.TempDir(), "config.yml")
	c.SetConfigFile(path)
	c.SetUIConfiguration(map[string]interface{}{
		"theme": "dark",
		"defaultFilters": map[string]interface{}{
			"scenes": map[string]interface{}{"mode": "SCENES", "ui_options": map[string]interface{}{"display_mode": 2}},
		},
	})
	source, err := DefaultFilterImportSource(c.GetUIConfiguration())
	require.NoError(t, err)
	require.NoError(t, c.PublishDefaultFilterImport(source))
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	c = InitializeEmpty()
	require.NoError(t, c.load(path))
	require.Equal(t, map[string]interface{}{"theme": "dark"}, c.GetUIConfiguration())
	require.NoError(t, c.PublishDefaultFilterImport(source))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "retry after restart must not republish the file")
	require.ErrorContains(t, c.PublishDefaultFilterImport(`{"defaultFilters":{}}`), "changed after staging")
}
