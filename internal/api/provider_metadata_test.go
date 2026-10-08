package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/scene"
	"github.com/stretchr/testify/require"
)

type providerReviewHooks struct{ fields [][]string }

func (h *providerReviewHooks) ExecutePostHooks(_ context.Context, _ int, _ hook.TriggerEnum, _ interface{}, fields []string) {
	h.fields = append(h.fields, fields)
}

func TestProviderMetadataInteractiveMutations(t *testing.T) {
	for _, kind := range []models.ArchiveEntityKind{models.ArchiveScene, models.ArchivePerformer, models.ArchiveStudio, models.ArchiveTag} {
		t.Run(string(kind), func(t *testing.T) {
			db, repo, _ := nativeMetadataReviewFixture(t)
			hooks := &providerReviewHooks{}
			resolver := &Resolver{repository: repo, hookExecutor: hooks, sceneService: &scene.Service{Repository: repo.Scene, PluginCache: plugin.NewCache(config.GetInstance())}}
			server := handler.New(NewExecutableSchema(Config{Resolvers: resolver}))
			server.AddTransport(transport.POST{})
			field := "name"
			if kind == models.ArchiveScene {
				field = "title"
			}
			entityType := strings.ToUpper(string(kind[:1])) + string(kind[1:])
			selection := func(endpoint string, fields ...string) map[string]any {
				return map[string]any{"endpoint": endpoint, "remote_id": "remote-entity", "fields": append([]string{}, fields...)}
			}
			const endpoint = "https://provider.invalid/graphql"
			request := func(operation string, input map[string]any, wantError bool) string {
				t.Helper()
				body, err := json.Marshal(map[string]any{
					"query":     fmt.Sprintf("mutation($input: %s%sInput!) { result: %s%s(input: $input) { id %s } }", entityType, operation, kind, operation, field),
					"variables": map[string]any{"input": input},
				})
				require.NoError(t, err)
				r := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				server.ServeHTTP(w, r)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				var result struct {
					Data   struct{ Result map[string]string } `json:"data"`
					Errors []any                              `json:"errors"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
				if wantError {
					require.NotEmpty(t, result.Errors, w.Body.String())
					return ""
				}
				require.Empty(t, result.Errors, w.Body.String())
				return result.Data.Result["id"]
			}
			id := request("Create", map[string]any{field: "  Imported name  ", "provider_metadata": []any{selection(endpoint, field)}}, false)
			localID, err := strconv.Atoi(id)
			require.NoError(t, err)
			var entity *models.ArchiveEntity
			history := func() []models.ProviderMetadataImport {
				t.Helper()
				var rows []models.ProviderMetadataImport
				require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					var err error
					entity, err = repo.ArchiveEntity.FindByLocalID(ctx, kind, localID)
					if err != nil {
						return err
					}
					rows, err = repo.ProviderMetadata.History(ctx, entity.UUID, 0, 100)
					return err
				}))
				return rows
			}
			rows := history()
			require.Len(t, rows, 1)
			require.Equal(t, "review", rows[0].Operation)
			require.JSONEq(t, fmt.Sprintf(`{%q:"Imported name"}`, field), string(rows[0].Values))
			original := rows[0]
			request("Update", map[string]any{"id": id, field: "Manually edited"}, false)
			require.Equal(t, []models.ProviderMetadataImport{original}, history())
			request("Update", map[string]any{"id": id, field: "Second choice", "provider_metadata": []any{selection("https://second.invalid/graphql", field)}}, false)
			rows = history()
			require.Len(t, rows, 2)
			require.Equal(t, original, rows[0])
			require.JSONEq(t, fmt.Sprintf(`{%q:"Second choice"}`, field), string(rows[1].Values))
			for _, bad := range []struct {
				name       string
				selections []any
				omit       bool
			}{
				{"missing field", []any{selection(endpoint, field)}, true},
				{"not metadata", []any{selection(endpoint, "favorite")}, false},
				{"wrong entity field", []any{selection(endpoint, "photographer")}, false},
				{"empty fields", []any{selection(endpoint)}, false},
				{"duplicate field", []any{selection(endpoint, field, field)}, false},
				{"duplicate source", []any{selection(endpoint, field), selection(endpoint, field)}, false},
				{"bad endpoint after valid receipt", []any{selection(endpoint, field), selection("https://user:secret@provider.invalid/graphql", field)}, false},
			} {
				t.Run(bad.name, func(t *testing.T) {
					input := map[string]any{"id": id, field: "Must roll back", "provider_metadata": bad.selections}
					if bad.omit {
						delete(input, field)
					}
					hookCount := len(hooks.fields)
					request("Update", input, true)
					require.Len(t, hooks.fields, hookCount)
					require.Equal(t, rows, history())
					require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
						var value string
						switch kind {
						case models.ArchiveScene:
							v, err := repo.Scene.Find(ctx, localID)
							require.NoError(t, err)
							value = v.Title
						case models.ArchivePerformer:
							v, err := repo.Performer.Find(ctx, localID)
							require.NoError(t, err)
							value = v.Name
						case models.ArchiveStudio:
							v, err := repo.Studio.Find(ctx, localID)
							require.NoError(t, err)
							value = v.Name
						case models.ArchiveTag:
							v, err := repo.Tag.Find(ctx, localID)
							require.NoError(t, err)
							value = v.Name
						}
						require.Equal(t, "Second choice", value)
						return nil
					}))
				})
			}
			countRows := func() [][]interface{} {
				var rows [][]interface{}
				require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					var err error
					_, rows, err = db.QuerySQL(ctx, fmt.Sprintf("SELECT count(*) FROM %ss", kind), nil)
					return err
				}))
				return rows
			}
			before := countRows()
			request("Create", map[string]any{field: "Must not exist", "provider_metadata": []any{selection("bad endpoint", field)}}, true)
			require.Equal(t, before, countRows())
			for _, fields := range hooks.fields {
				require.NotContains(t, fields, "provider_metadata")
			}
		})
	}
}

func TestProviderMetadataSelectedFieldMappings(t *testing.T) {
	for _, tt := range []struct {
		kind         models.ArchiveEntityKind
		field, input string
	}{
		{models.ArchiveScene, "image", "cover_image"}, {models.ArchivePerformer, "image", "image_input"},
		{models.ArchivePerformer, "height", "height_cm"}, {models.ArchiveScene, "performers", "performer_ids"},
		{models.ArchiveScene, "studio", "studio_id"}, {models.ArchiveStudio, "tags", "tag_ids"},
		{models.ArchiveStudio, "parent", "parent_id"}, {models.ArchiveTag, "parents", "parent_ids"},
	} {
		require.True(t, providerFieldIncluded(tt.kind, tt.field, changesetTranslator{inputMap: map[string]interface{}{tt.input: nil}}))
		require.False(t, providerFieldIncluded(tt.kind, tt.field, changesetTranslator{}))
	}
}
