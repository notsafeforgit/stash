package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJQMappingSemantics(t *testing.T) {
	input := map[string]interface{}{"title": "Example", "people": []string{"Alice", "Bob"}, "large": json.Number("9007199254740993")}
	result, err := EvaluateMappings(context.Background(), map[string]interface{}{
		"title": ".title | ascii_upcase", "people": ".people | join(\", \")", "skip": "empty", "clear": "null", "large": ".large + 1",
	}, input)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, `{"title":"EXAMPLE","people":"Alice, Bob","clear":null,"large":9007199254740994}`, string(encoded))
	require.Equal(t, "Example", input["title"])
	for _, expression := range []string{"1, 2", ".title | .bad", "unknown_function", ".["} {
		_, err := EvaluateMappings(context.Background(), map[string]interface{}{"field": expression}, input)
		require.Error(t, err, expression)
	}
}

func TestJQLimitsAndIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := EvaluateJQ(ctx, "def loop: loop; loop", nil)
	require.Error(t, err)
	_, err = EvaluateJQ(context.Background(), "range(2000)", nil)
	require.ErrorContains(t, err, "more than")
	_, err = EvaluateJQ(context.Background(), ".", strings.Repeat("x", jqMaxBytes))
	require.ErrorContains(t, err, "input exceeds")
	_, err = EvaluateJQ(context.Background(), `import "missing" as m; m::x`, nil)
	require.Error(t, err)
	result, err := EvaluateJQ(context.Background(), "env", nil)
	require.NoError(t, err)
	require.Equal(t, []interface{}{map[string]interface{}{}}, result)
	result, err = EvaluateJQ(context.Background(), "null, false, []", nil)
	require.NoError(t, err)
	require.Equal(t, []interface{}{nil, false, []interface{}{}}, result)
}

func TestPluginMappingsUseStrictUTCDateFilter(t *testing.T) {
	mapping := map[string]interface{}{"date": ".published_at | utc_date | select(. != null)"}
	result, err := EvaluateMappings(context.Background(), mapping, map[string]interface{}{"published_at": "2026-09-29T22:37:08.123456789-07:00"})
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{"date": "2026-09-30"}, result)
	result, err = EvaluateMappings(context.Background(), mapping, map[string]interface{}{})
	require.NoError(t, err)
	require.Empty(t, result)
	_, err = EvaluateMappings(context.Background(), mapping, map[string]interface{}{"published_at": "2026-02-30"})
	require.ErrorContains(t, err, "utc_date")
}

func TestSettingManifestAndValidation(t *testing.T) {
	c, err := loadPluginFromYAML(strings.NewReader(`apiVersion: 3
name: Fixture
settings:
  enabled:
    type: BOOLEAN
    default: false
  amount:
    type: NUMBER
    default: 0
  mode:
    type: STRING
    editor: SELECT
    default: import
    options:
      - {value: import, label: Import}
      - {value: both, label: Both}
  mappings:
    type: STRING
    editor: JQ_MAP
    default: '{"title":".title"}'
`))
	require.NoError(t, err)
	p := c.toPlugin()
	values := p.SettingsValuesV3(map[string]interface{}{"enabled": true, "unrelated": "kept"})
	require.Equal(t, true, values["enabled"])
	require.Equal(t, 0, values["amount"])
	require.Equal(t, "import", values["mode"])
	require.NoError(t, p.ValidateSettingsV3(map[string]interface{}{"enabled": false, "amount": 2.5, "mode": "both", "mappings": `{"clear":"null","skip":"empty"}`}, nil))
	for key, value := range map[string]interface{}{"enabled": "false", "amount": "2", "mode": "bad", "mappings": `{"bad":".["}`, "unknown": true} {
		require.Error(t, p.ValidateSettingsV3(map[string]interface{}{key: value}, nil))
	}
	require.Error(t, p.ValidateSettingsV3(nil, []string{"unknown"}))
	require.Error(t, p.ValidateSettingsV3(map[string]interface{}{"mode": "import"}, []string{"mode"}))
	_, err = loadPluginFromYAML(strings.NewReader("apiVersion: 3\nname: Fixture\nsettings:\n  bad:\n    type: STRING\n    editor: JQ_MAP\n    default: 'null'\n"))
	require.Error(t, err)
}
