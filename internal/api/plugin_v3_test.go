package api

import (
	"bytes"
	"context"
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
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stretchr/testify/require"
)

func TestV3PluginAPIIsolation(t *testing.T) {
	cfg := config.InitializeEmpty()
	dir := t.TempDir()
	cfg.SetConfigFile(filepath.Join(t.TempDir(), "config.yml"))
	for name, manifest := range map[string]string{
		"legacy": "name: Legacy\ntasks: [{name: Legacy task}]\nsettings: {enabled: {type: BOOLEAN}}\n",
		"native": `apiVersion: 3
name: Native
ui: {entry: index.js, assets: {"/": dist}}
tasks: [{name: Native task}]
hooks: [{name: File deleted, triggeredBy: [File.Destroy.Post]}]
settings:
  mappings:
    type: JSON
    editor: JQ_MAP
    default: {title: '.title'}
    mappingTargets: [{name: title, label: Title, type: String, description: The title}]
  data: {type: JSON, default: [false, 0, null]}
`,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".yml"), []byte(manifest), 0600))
	}
	cache := plugin.NewCache(pluginSettingsTestConfig{cfg, dir})
	cache.ReloadPlugins()
	manager.SetInstance(&manager.Manager{PluginCache: cache})
	t.Cleanup(func() { manager.SetInstance(nil); config.InitializeEmpty() })
	require.True(t, cache.HasHooks(hook.FileDestroyPost), "v3-only hooks remain available to every backend caller")
	server := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{}}))
	server.AddTransport(transport.POST{})
	request := func(query string, variables map[string]interface{}) map[string]interface{} {
		body, err := json.Marshal(map[string]interface{}{"query": query, "variables": variables})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), BaseURLCtxKey, "https://example.test/stash"))
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		require.Equal(t, http.StatusOK, response.Code)
		var ret map[string]interface{}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &ret))
		return ret
	}
	response := request(`{
 plugins { id settings { type } }
 pluginTasks { plugin { id } }
 pluginsV3 { id api_version paths { entry } settings { type options { value } } }
 pluginTasksV3 { plugin { id api_version } }
 pluginSettingsV3(plugin_id: "native") { values definitions { name type options { value } mapping_targets { name label type description } } }
}`, nil)
	require.NotContains(t, response, "errors")
	data := response["data"].(map[string]interface{})
	legacy := data["plugins"].([]interface{})
	require.Len(t, legacy, 1)
	require.Equal(t, "legacy", legacy[0].(map[string]interface{})["id"])
	require.Len(t, data["pluginTasks"], 1)
	require.Len(t, data["pluginTasksV3"], 2)
	plugins := data["pluginsV3"].([]interface{})
	require.Len(t, plugins, 2)
	native := plugins[1].(map[string]interface{})
	require.Equal(t, float64(3), native["api_version"])
	require.Equal(t, "https://example.test/stash/plugin/native/assets/index.js", native["paths"].(map[string]interface{})["entry"])
	settings := data["pluginSettingsV3"].(map[string]interface{})
	require.Equal(t, map[string]interface{}{"title": ".title"}, settings["values"].(map[string]interface{})["mappings"])
	require.Empty(t, settings["definitions"].([]interface{})[0].(map[string]interface{})["options"])
	for _, definition := range settings["definitions"].([]interface{}) {
		definition := definition.(map[string]interface{})
		if definition["name"] == "mappings" {
			require.Equal(t, []interface{}{map[string]interface{}{"name": "title", "label": "Title", "type": "String", "description": "The title"}}, definition["mapping_targets"])
		}
	}
	require.Contains(t, request(`{ pluginSettings(plugin_id: "native") { values } }`, nil), "errors")
	require.Contains(t, request(`mutation { updatePluginSettings(plugin_id: "native", input: {data: null}) }`, nil), "errors")
	const update = `mutation($input: Map!, $reset: [String!]) { updatePluginSettingsV3(plugin_id: "native", input: $input, reset: $reset) }`
	response = request(update, map[string]interface{}{"input": map[string]interface{}{"mappings": map[string]interface{}{"title": ".catalog.title"}, "data": nil}})
	require.NotContains(t, response, "errors")
	saved := cfg.GetPluginConfiguration("native")
	require.Equal(t, map[string]interface{}{"title": ".catalog.title"}, saved["mappings"])
	require.Contains(t, saved, "data")
	require.Nil(t, saved["data"])
	response = request(update, map[string]interface{}{"input": map[string]interface{}{"data": true, "mappings": map[string]interface{}{"bad": ".["}}})
	require.Contains(t, response, "errors")
	require.Equal(t, saved, cfg.GetPluginConfiguration("native"), "a bad mapping must reject the whole patch")
	response = request(update, map[string]interface{}{"input": map[string]interface{}{"data": true, "mappings": map[string]interface{}{"id": "empty"}}})
	require.Contains(t, response, "errors")
	require.Equal(t, saved, cfg.GetPluginConfiguration("native"), "unsupported targets must reject the whole patch")
	response = request(update, map[string]interface{}{"input": map[string]interface{}{}, "reset": []string{"mappings"}})
	require.NotContains(t, response, "errors")
	require.NotContains(t, cfg.GetPluginConfiguration("native"), "mappings")
	nativeJSON := map[string]interface{}{"dotted.key": []interface{}{false, float64(0), nil, map[string]interface{}{"nested": "value"}}}
	response = request(update, map[string]interface{}{"input": map[string]interface{}{"data": nativeJSON}})
	require.NotContains(t, response, "errors")
	response = request(`{ pluginSettingsV3(plugin_id: "native") { values } }`, nil)
	require.NotContains(t, response, "errors")
	effective := response["data"].(map[string]interface{})["pluginSettingsV3"].(map[string]interface{})["values"].(map[string]interface{})
	require.Equal(t, nativeJSON, effective["data"], "native JSON must preserve keys, arrays, false, zero and null")
	response = request(`mutation { updatePluginSettingsV3(plugin_id: "legacy", input: {enabled: true}) }`, nil)
	require.NotContains(t, response, "errors")
	require.Equal(t, true, cfg.GetPluginConfiguration("legacy")["enabled"])
}
