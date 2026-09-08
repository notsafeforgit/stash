package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stretchr/testify/require"
)

func TestConfigurationJSONNumbersPersistAcrossReload(t *testing.T) {
	const rows = `[
		{"__typename":"SavedFilter","savedFilterId":123},
		{"__typename":"CustomFilter","mode":"SCENES","sortBy":"date","direction":"DESC"},
		{"__typename":"SavedFilter","savedFilterId":"456"},
		{"__typename":"SavedFilter","savedFilterId":789,"extension":[null,[],[1,{"weight":2.5,"tiny":1e-7,"count":1e3}]]}
	]`
	const ui = `{"frontPageContent":` + rows + `,"unrelated":"keep"}`

	for _, tt := range []struct {
		name         string
		query        string
		variables    map[string]interface{}
		persistedKey string
		want         string
	}{
		{
			name:         "single_setting_array",
			query:        `mutation($value: Any) { result: configureUISetting(key: "frontPageContent", value: $value) }`,
			variables:    map[string]interface{}{"value": json.RawMessage(rows)},
			persistedKey: config.UI,
			want:         ui,
		},
		{
			name:         "legacy_partial_config",
			query:        `mutation($partial: Map) { result: configureUI(partial: $partial) }`,
			variables:    map[string]interface{}{"partial": json.RawMessage(`{"frontPageContent":` + rows + `}`)},
			persistedKey: config.UI,
			want:         ui,
		},
		{
			name:         "legacy_full_config",
			query:        `mutation($input: Map) { result: configureUI(input: $input) }`,
			variables:    map[string]interface{}{"input": json.RawMessage(ui)},
			persistedKey: config.UI,
			want:         ui,
		},
		{
			name:         "plugin_nested_arrays",
			query:        `mutation($input: Map!) { result: configurePlugin(plugin_id: "fixture", input: $input) }`,
			variables:    map[string]interface{}{"input": json.RawMessage(ui)},
			persistedKey: config.PluginsSettingPrefix + "fixture",
			want:         ui,
		},
		{
			name:         "clear_setting",
			query:        `mutation($value: Any) { result: configureUISetting(key: "frontPageContent", value: $value) }`,
			variables:    map[string]interface{}{"value": nil},
			persistedKey: config.UI,
			want:         `{"frontPageContent":null,"unrelated":"keep"}`,
		},
		{
			name:         "intentionally_empty_home_screen",
			query:        `mutation($value: Any) { result: configureUISetting(key: "frontPageContent", value: $value) }`,
			variables:    map[string]interface{}{"value": json.RawMessage(`[]`)},
			persistedKey: config.UI,
			want:         `{"frontPageContent":[],"unrelated":"keep"}`,
		},
		{
			name:         "numeric_scalar_setting",
			query:        `mutation($value: Any) { result: configureUISetting(key: "previewVolume", value: $value) }`,
			variables:    map[string]interface{}{"value": json.RawMessage(`1e1`)},
			persistedKey: config.UI,
			want:         `{"previewVolume":10,"unrelated":"keep"}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Resolver configuration is process-global; these cases must not run
			// in parallel. Every write targets a private temporary configuration.
			cfg := config.InitializeEmpty()
			t.Cleanup(func() { config.InitializeEmpty() })
			path := filepath.Join(t.TempDir(), "config.yml")
			cfg.SetConfigFile(path)
			cfg.SetUIConfiguration(map[string]interface{}{"unrelated": "keep"})

			server := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{}}))
			server.AddTransport(transport.POST{})
			body, err := json.Marshal(map[string]interface{}{"query": tt.query, "variables": tt.variables})
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			require.JSONEq(t, `{"data":{"result":`+tt.want+`}}`, response.Body.String())

			// Use the same loader/parser as Config.load. JSON responses alone
			// cannot detect json.Number becoming a string during YAML persistence.
			reloaded := koanf.New(".")
			require.NoError(t, reloaded.Load(file.Provider(path), yaml.Parser()))
			persisted, err := json.Marshal(reloaded.Get(tt.persistedKey))
			require.NoError(t, err)
			require.JSONEq(t, tt.want, string(persisted))
		})
	}
}
