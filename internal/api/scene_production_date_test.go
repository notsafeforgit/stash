package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestBulkSceneProductionDate(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	for _, mode := range []string{"legacy", "job IDs", "job filter"} {
		t.Run(mode, func(t *testing.T) {
			db := sqlite.NewDatabase()
			require.NoError(t, db.Open(filepath.Join(t.TempDir(), "stash.sqlite")))
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			repository := db.Repository()
			updater := &customFieldsBulkUpdater{}
			resolver := &Resolver{repository: repository, hookExecutor: &mockHookExecutor{}, bulkUpdater: updater}
			server := handler.New(NewExecutableSchema(Config{Resolvers: resolver}))
			server.AddTransport(transport.POST{})
			var ids []int
			initial, err := models.ParseDate("2010-06-15")
			require.NoError(t, err)
			require.NoError(t, repository.WithTxn(testCtx, func(ctx context.Context) error {
				for i := range 3 {
					scene := models.NewScene()
					scene.Title = fmt.Sprintf("production date target %d", i)
					scene.ProductionDate = &initial
					if err := repository.Scene.Create(ctx, &scene, nil); err != nil {
						return err
					}
					ids = append(ids, scene.ID)
				}
				return nil
			}))

			for _, change := range []struct {
				name, value, want string
				include, clear    bool
			}{
				{name: "omitted", want: "2010-06-15"},
				{name: "month precision", include: true, value: "2001-03", want: "2001-03"},
				{name: "year precision", include: true, value: "2000", want: "2000"},
				{name: "clear", include: true, clear: true},
			} {
				t.Run(change.name, func(t *testing.T) {
					*updater = customFieldsBulkUpdater{}
					input := map[string]interface{}{"ids": []string{strconv.Itoa(ids[0]), strconv.Itoa(ids[1])}}
					if change.include {
						input["production_date"] = change.value
						if change.clear {
							input["production_date"] = nil
						}
					}
					mutation, selection := "bulkSceneUpdate", "{ id }"
					if mode != "legacy" {
						mutation, selection = "bulkSceneUpdateJob", ""
					}
					if mode == "job filter" {
						input["ids"] = []string{}
						input["apply_to_items_matching_filters"] = true
						input["scene_filter_ast"] = map[string]interface{}{"root": map[string]interface{}{"condition": map[string]interface{}{
							"field": "title", "value": map[string]interface{}{"value": "production date target [01]$", "modifier": "MATCHES_REGEX"},
						}}}
					}
					body, err := json.Marshal(map[string]interface{}{
						"query":     fmt.Sprintf("mutation($input: BulkSceneUpdateInput!) { result: %s(input: $input) %s }", mutation, selection),
						"variables": map[string]interface{}{"input": input},
					})
					require.NoError(t, err)
					request := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
					request.Header.Set("Content-Type", "application/json")
					response := httptest.NewRecorder()
					server.ServeHTTP(response, request)
					require.Equal(t, http.StatusOK, response.Code)
					require.NotContains(t, response.Body.String(), `"errors"`)
					if mode == "job filter" {
						require.JSONEq(t, `{"data":{"result":"1"}}`, response.Body.String())
						require.ElementsMatch(t, ids[:2], updater.ids)
						for _, id := range updater.ids {
							require.NoError(t, repository.WithTxn(testCtx, func(ctx context.Context) error {
								return updater.operation.Update(ctx, id)
							}))
						}
					}
					require.NoError(t, repository.WithReadTxn(testCtx, func(ctx context.Context) error {
						for i, id := range ids {
							scene, err := repository.Scene.Find(ctx, id)
							require.NoError(t, err)
							want := change.want
							if i == 2 {
								want = initial.String()
							}
							if want == "" {
								require.Nil(t, scene.ProductionDate)
							} else {
								require.NotNil(t, scene.ProductionDate)
								require.Equal(t, want, scene.ProductionDate.String())
							}
						}
						return nil
					}))
				})
			}
		})
	}
}
