package plugin

import (
	"strings"
	"testing"

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
	require.Empty(t, p.Settings, "v3 settings must not be projected into the v2.5 model")
	require.Equal(t, "index.js", p.UI.Entry)
	require.Empty(t, p.UI.Javascript)
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

func TestLegacyManifestAdapter(t *testing.T) {
	c, err := loadPluginFromYAML(strings.NewReader(`name: Legacy
ui: {javascript: [legacy.js]}
settings:
  enabled: {type: BOOLEAN}
`))
	require.NoError(t, err)
	p := c.toPlugin()
	require.Equal(t, 2, p.APIVersion)
	require.Len(t, p.Settings, 1)
	require.Len(t, p.SettingsV3, 1)
	require.Equal(t, PluginSettingTypeV3("BOOLEAN"), p.SettingsV3[0].Type)
	require.Empty(t, p.UI.Entry)
	require.NoError(t, p.ValidateSettingsV3(map[string]interface{}{"enabled": true}, nil))
}
