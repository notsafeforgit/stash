package plugin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestV3MappingTargets(t *testing.T) {
	for _, settingType := range []string{"JSON", "STRING"} {
		t.Run(settingType, func(t *testing.T) {
			c, err := loadPluginFromYAML(strings.NewReader(`apiVersion: 3
name: Restricted mapping
settings:
  mapping:
    type: ` + settingType + `
    editor: JQ_MAP
    mappingTargets:
      - {name: title, label: Title, type: String, description: Scene title}
      - {name: urls, label: URLs, type: '[String!]'}
`))
			require.NoError(t, err)
			p := c.toPlugin()
			require.Len(t, p.SettingsV3[0].MappingTargets, 2)
			value := func(mappings map[string]interface{}) interface{} {
				if settingType == "STRING" {
					encoded, err := json.Marshal(mappings)
					require.NoError(t, err)
					return string(encoded)
				}
				return mappings
			}
			for _, mappings := range []map[string]interface{}{{}, {"title": "empty"}, {"urls": "null"}, {"title": ".stash.title", "urls": "[.catalog.url]"}} {
				require.NoError(t, p.ValidateSettingsV3(map[string]interface{}{"mapping": value(mappings)}, nil))
			}
			for _, target := range []string{"id", "payload", "title.typo"} {
				err = p.ValidateSettingsV3(map[string]interface{}{"mapping": value(map[string]interface{}{target: "empty"})}, nil)
				require.ErrorContains(t, err, "unsupported mapping target")
			}
			saved := map[string]interface{}{"mapping": map[string]interface{}{"removed_field": ".stash.title"}}
			require.Equal(t, saved, p.SettingsValuesV3(saved), "old targets remain visible for repair")
		})
	}
}

func TestV3MappingTargetDeclarations(t *testing.T) {
	for _, declaration := range []string{
		"editor: TEXT\n    mappingTargets: [{name: title, label: Title, type: String}]",
		"editor: JQ_MAP\n    mappingTargets: []",
		"editor: JQ_MAP\n    mappingTargets: [{name: title, label: Title, type: String}, {name: title, label: Duplicate, type: String}]",
		"editor: JQ_MAP\n    mappingTargets: [{name: ' ', label: Title, type: String}]",
		"editor: JQ_MAP\n    mappingTargets: [{name: title, type: String}]",
		"editor: JQ_MAP\n    mappingTargets: [{name: title, label: Title}]",
		"editor: JQ_MAP\n    mappingTargets: [{name: title, label: Title, type: String}]\n    default: '{\"id\": \"empty\"}'",
	} {
		_, err := loadPluginFromYAML(strings.NewReader("apiVersion: 3\nname: Bad\nsettings:\n  mapping:\n    type: STRING\n    " + declaration))
		require.Error(t, err, declaration)
	}
}
