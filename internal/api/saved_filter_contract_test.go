package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSavedFilterNativeAPIContract(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "filters.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	server := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{repository: db.Repository()}}))
	server.AddTransport(transport.POST{})

	type response struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	request := func(query string, variables map[string]any) response {
		t.Helper()
		body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)
		require.Contains(t, []int{http.StatusOK, http.StatusUnprocessableEntity}, recorder.Code, recorder.Body.String())
		var result response
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
		return result
	}
	const selection = `id name mode find_filter { q sort direction per_page } filter_ast ui_options`
	const save = `mutation($input: SaveFilterInput!) { filter: saveFilter(input: $input) { ` + selection + ` } }`
	const find = `query($id: ID!) { filter: findSavedFilter(id: $id) { ` + selection + ` } }`
	const ast = `{"root":{"group":{"operator":"OR","children":[
		{"condition":{"field":"title","value":{"modifier":"INCLUDES","value":"alpha"}}},
		{"group":{"operator":"AND","children":[
			{"condition":{"field":"title","value":{"modifier":"EXCLUDES","value":"beta"}}},
			{"condition":{"field":"performers","value":{"modifier":"INCLUDES","value":[{"id":"42","label":"Retained performer label"}]}}}
		]}}
	]}}}`
	input := map[string]any{
		"name": "  Native filter  ", "mode": "SCENES", "filter_ast": json.RawMessage(ast),
		"find_filter": map[string]any{"q": "search", "sort": "title", "direction": "ASC", "per_page": 40},
		"ui_options":  map[string]any{"custom": "retained"},
	}
	created := request(save, map[string]any{"input": input})
	require.Empty(t, created.Errors)
	var stored struct {
		ID   string          `json:"id"`
		Name string          `json:"name"`
		AST  json.RawMessage `json:"filter_ast"`
	}
	require.NoError(t, json.Unmarshal(created.Data["filter"], &stored))
	require.NotEmpty(t, stored.ID)
	require.Equal(t, "Native filter", stored.Name)
	require.JSONEq(t, ast, string(stored.AST))
	checkStored := func(want json.RawMessage) {
		t.Helper()
		found := request(find, map[string]any{"id": stored.ID})
		require.Empty(t, found.Errors)
		require.JSONEq(t, string(want), string(found.Data["filter"]))
	}
	checkStored(created.Data["filter"])

	// Editing a nested OR with repeated fields must really save, including labels.
	input["id"], input["name"] = stored.ID, "Edited native filter"
	updatedAST := strings.ReplaceAll(ast, "alpha", "updated")
	input["filter_ast"] = json.RawMessage(updatedAST)
	updated := request(save, map[string]any{"input": input})
	require.Empty(t, updated.Errors)
	require.NoError(t, json.Unmarshal(updated.Data["filter"], &stored))
	require.Equal(t, "Edited native filter", stored.Name)
	require.JSONEq(t, updatedAST, string(stored.AST))
	checkStored(updated.Data["filter"])

	input["name"] = "Must not overwrite"
	input["filter_ast"] = json.RawMessage(`{"root":{"group":{"operator":"OR","children":[]}}}`)
	rejected := request(save, map[string]any{"input": input})
	require.NotEmpty(t, rejected.Errors)
	checkStored(updated.Data["filter"])

	// Retired calls fail validation rather than returning a lossy view, ignoring
	// writes, or creating an independent YAML default that native readers ignore.
	for _, query := range []string{
		`query($id: ID!) { findSavedFilter(id: $id) { filter } }`,
		`query($id: ID!) { findSavedFilter(id: $id) { object_filter } }`,
		`mutation { saveFilter(input: {name: "old", mode: SCENES, object_filter: {title: "old"}}) { id } }`,
		`query { findDefaultFilter(mode: SCENES) { id } }`,
		`mutation { setDefaultFilter(input: {mode: SCENES}) }`,
		`mutation { migrateLegacySavedFilters }`,
	} {
		t.Run(query, func(t *testing.T) {
			result := request(query, map[string]any{"id": stored.ID})
			require.NotEmpty(t, result.Errors)
			require.Empty(t, result.Data)
		})
	}
	checkStored(updated.Data["filter"])

	input["name"], input["filter_ast"] = "Cleared", nil
	cleared := request(save, map[string]any{"input": input})
	require.Empty(t, cleared.Errors)
	require.NoError(t, json.Unmarshal(cleared.Data["filter"], &stored))
	require.Equal(t, "null", string(stored.AST))
	checkStored(cleared.Data["filter"])
}
