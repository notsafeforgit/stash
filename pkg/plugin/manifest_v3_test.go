package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stretchr/testify/require"
)

func TestV3ManifestContract(t *testing.T) {
	c, err := loadPluginFromYAML(strings.NewReader(`apiVersion: 3
name: Native settings
ui:
  entry: index.js
  assets: {"/": dist}
settings:
  mappings:
    type: JSON
    editor: JQ_MAP
    default: {title: '.catalog.title'}
  filters:
    type: JSON
    default: [false, 0, null, {field: title}]
  enabled: {type: BOOLEAN, default: false}
`))
	require.NoError(t, err)
	p := c.toPlugin()
	require.Equal(t, 3, p.APIVersion)
	require.Equal(t, "index.js", p.UI.Entry)
	values := p.SettingsValuesV3(nil)
	require.Equal(t, map[string]interface{}{"title": ".catalog.title"}, values["mappings"])
	require.Equal(t, []interface{}{false, 0, nil, map[string]interface{}{"field": "title"}}, values["filters"])
	require.NoError(t, p.ValidateSettingsV3(map[string]interface{}{"mappings": map[string]interface{}{"clear": "null", "skip": "empty"}, "filters": nil}, nil))
	for key, value := range map[string]interface{}{
		"mappings": `{"title":".title"}`, "enabled": "false", "unknown": true,
	} {
		require.Error(t, p.ValidateSettingsV3(map[string]interface{}{key: value}, nil))
	}
	require.Error(t, p.ValidateSettingsV3(map[string]interface{}{"mappings": map[string]interface{}{"title": ".["}}, nil))
	require.Error(t, p.ValidateSettingsV3(map[string]interface{}{"enabled": true}, []string{"enabled"}))
	saved := map[string]interface{}{"mappings": `{"title":".stash.title"}`}
	require.Equal(t, map[string]interface{}{"title": ".stash.title"}, p.SettingsValuesV3(saved)["mappings"])
	require.Equal(t, `{"title":".stash.title"}`, saved["mappings"], "reading old mapping overrides must not mutate storage")
}

func TestV3ManifestRejectsUnsupportedContracts(t *testing.T) {
	for _, manifest := range []string{
		"apiVersion: 4\nname: Future",
		"apiVersion: 0\nname: Unknown",
		"apiVersion: 3.5\nname: Invalid version",
		"apiVersion: null\nname: Invalid version",
		"apiVersion: '3'\nname: Invalid version",
		"apiVersion: banana\nname: Unknown",
		"apiVersion: 3\nname: Typo\nunknownField: true",
		"apiVersion: 3\nname: Legacy UI\nui: {javascript: [legacy.js]}",
		"apiVersion: 3\nname: Legacy CSS\nui: {css: [legacy.css]}",
		"apiVersion: 3\nsettings: {mapping: {type: JSON, editor: JQ_MAP, default: {title: '.['}}}",
		"apiVersion: 3\nsettings: {data: {type: JSON, default: {1: value}}}",
		"apiVersion: 3\nsettings: {data: {type: NUMBER, editor: JSON}}",
		"apiVersion: 3\nhooks: [{name: Typo, triggeredBy: [File.Creation.Post]}]",
		"apiVersion: 3\ntasks: [null]",
	} {
		_, err := loadPluginFromYAML(strings.NewReader(manifest))
		require.Error(t, err, manifest)
	}
}

func TestUnversionedManifestIsRejected(t *testing.T) {
	c, err := loadPluginFromYAML(strings.NewReader(`name: Legacy
ui: {javascript: [legacy.js]}
settings:
  enabled: {type: BOOLEAN}
`))
	require.Nil(t, c)
	require.ErrorContains(t, err, "requires apiVersion: 3")
}

func TestNativePluginExamplesDeclareSupportedManifests(t *testing.T) {
	paths, err := filepath.Glob("examples/*/*.yml")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			c, err := loadPluginFromYAML(strings.NewReader(string(body)))
			require.NoError(t, err)
			require.Equal(t, 3, c.toPlugin().APIVersion)
		})
	}
}

type manifestServerConfig struct {
	durableHookConfig
	directory string
}

func (c manifestServerConfig) GetPluginsPath() string { return c.directory }

func TestPluginReloadReportsRejectedManifests(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"legacy":  "name: Legacy\nhooks: [{name: Unavailable, triggeredBy: [Scene.Create.Post]}]\n",
		"future":  "apiVersion: 4\nname: Future\n",
		"invalid": "apiVersion: 3\nui: {javascript: [old.js]}\n",
		"native":  "apiVersion: 3\nname: Native\nhooks: [{name: Native hook, triggeredBy: [Scene.Update.Post]}]\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".yml"), []byte(body), 0600))
	}
	cache := NewCache(manifestServerConfig{directory: dir})
	cache.ReloadPlugins()
	require.Len(t, cache.ListPlugins(), 1)
	require.True(t, cache.HasHooks(hook.SceneUpdatePost))
	require.False(t, cache.HasHooks(hook.SceneCreatePost), "rejected manifests must not register hooks")
	require.Len(t, cache.LoadErrors(), 3)
	errors := cache.LoadErrors()
	for i, path := range []string{"future.yml", "invalid.yml", "legacy.yml"} {
		require.Equal(t, path, errors[i].Path)
		require.NotEmpty(t, errors[i].Message)
	}
	errors[0].Message = "changed"
	require.NotEqual(t, "changed", cache.LoadErrors()[0].Message)
	for _, name := range []string{"future", "invalid", "legacy"} {
		require.Nil(t, cache.GetPlugin(name))
		require.NoError(t, os.Remove(filepath.Join(dir, name+".yml")))
	}
	cache.ReloadPlugins()
	require.Len(t, cache.ListPlugins(), 1)
	require.Empty(t, cache.LoadErrors())
}
