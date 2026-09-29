package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/plugin"
	"github.com/stretchr/testify/require"
)

type pluginSettingsTestConfig struct {
	*config.Config
	path string
}

func (c pluginSettingsTestConfig) GetPluginsPath() string { return c.path }

func TestPluginSettingsGraphQL(t *testing.T) {
	cfg := config.InitializeEmpty()
	dir := t.TempDir()
	cfg.SetConfigFile(filepath.Join(dir, "config.yml"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fixture.yml"), []byte(`name: Fixture
settings:
  enabled: {type: BOOLEAN, default: false}
  mappings: {type: STRING, editor: JQ_MAP, default: '{}'}
`), 0600))
	cache := plugin.NewCache(pluginSettingsTestConfig{cfg, dir})
	cache.ReloadPlugins()
	manager.SetInstance(&manager.Manager{PluginCache: cache})
	t.Cleanup(func() { manager.SetInstance(nil); config.InitializeEmpty() })
	server := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{}}))
	server.AddTransport(transport.POST{})
	request := func(query string, variables map[string]interface{}) map[string]interface{} {
		body, err := json.Marshal(map[string]interface{}{"query": query, "variables": variables})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		require.Equal(t, http.StatusOK, response.Code)
		var ret map[string]interface{}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &ret))
		return ret
	}
	response := request(`{ pluginSettings(plugin_id: "fixture") { values definitions { name default_value editor options { value label } } } }`, nil)
	require.NotContains(t, response, "errors")
	settings := response["data"].(map[string]interface{})["pluginSettings"].(map[string]interface{})
	require.Equal(t, map[string]interface{}{"enabled": false, "mappings": "{}"}, settings["values"])
	definitions := settings["definitions"].([]interface{})
	require.Equal(t, "JQ_MAP", definitions[1].(map[string]interface{})["editor"])
	require.Equal(t, []interface{}{}, definitions[1].(map[string]interface{})["options"])
	const update = `mutation($input: Map!, $reset: [String!]) { updatePluginSettings(plugin_id: "fixture", input: $input, reset: $reset) }`
	cfg.SetPluginConfiguration("fixture", map[string]interface{}{"legacy": "preserved"})
	response = request(update, map[string]interface{}{"input": map[string]interface{}{"enabled": true}})
	require.NotContains(t, response, "errors")
	require.Equal(t, "preserved", cfg.GetPluginConfiguration("fixture")["legacy"])
	response = request(update, map[string]interface{}{"input": map[string]interface{}{"enabled": false, "mappings": `{"bad":".["}`}})
	require.Contains(t, response, "errors")
	require.Equal(t, true, cfg.GetPluginConfiguration("fixture")["enabled"])
	response = request(update, map[string]interface{}{"input": map[string]interface{}{}, "reset": []string{"enabled"}})
	require.NotContains(t, response, "errors")
	require.NotContains(t, cfg.GetPluginConfiguration("fixture"), "enabled")
	response = request(`{ pluginEvaluateJQ(expression: "\"hello\" | ascii_upcase, null") }`, nil)
	require.NotContains(t, response, "errors")
	require.Equal(t, []interface{}{"HELLO", nil}, response["data"].(map[string]interface{})["pluginEvaluateJQ"])
	response = request(`query($m: Map!) { pluginEvaluateMappings(mappings: $m, input: {title: "hello"}) }`, map[string]interface{}{"m": map[string]interface{}{"title": ".title", "skip": "empty", "clear": "null"}})
	require.NotContains(t, response, "errors")
	require.Equal(t, map[string]interface{}{"title": "hello", "clear": nil}, response["data"].(map[string]interface{})["pluginEvaluateMappings"])
}
