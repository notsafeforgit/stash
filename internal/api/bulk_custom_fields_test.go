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
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// Capture the actual queued operation, then execute it outside the HTTP request
// using the same transaction boundary as a worker. No live library is involved.
type customFieldsBulkUpdater struct {
	ids       []int
	operation manager.BulkUpdateOperation
	fields    []string
}

func (u *customFieldsBulkUpdater) BulkUpdate(_ context.Context, _ string, ids []int, operation manager.BulkUpdateOperation, _ hook.TriggerEnum, _ interface{}, fields []string) int {
	u.ids = append([]int(nil), ids...)
	u.operation = operation
	u.fields = fields
	return 1
}

func TestBulkCustomFieldsPersistence(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })

	for _, entity := range []string{"Studio", "Tag"} {
		for _, background := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/job=%t", entity, background), func(t *testing.T) {
				db := sqlite.NewDatabase()
				require.NoError(t, db.Open(filepath.Join(t.TempDir(), "stash.sqlite")))
				t.Cleanup(func() { require.NoError(t, db.Close()) })
				repository := db.Repository()
				updater := &customFieldsBulkUpdater{}
				resolver := &Resolver{repository: repository, hookExecutor: &mockHookExecutor{}, bulkUpdater: updater}
				server := handler.New(NewExecutableSchema(Config{Resolvers: resolver}))
				server.AddTransport(transport.POST{})

				var store interface {
					models.CustomFieldsReader
					models.CustomFieldsWriter
				}
				var ids []int
				names := []string{"bulk custom target A", "bulk custom target B", "unselected"}
				require.NoError(t, repository.WithTxn(testCtx, func(ctx context.Context) error {
					for _, name := range names {
						if entity == "Studio" {
							store = db.Studio
							input := models.NewCreateStudioInput()
							input.Name = name
							if err := repository.Studio.Create(ctx, &input); err != nil {
								return err
							}
							ids = append(ids, input.ID)
						} else {
							store = db.Tag
							tag := models.NewTag()
							tag.Name = name
							if err := repository.Tag.Create(ctx, &models.CreateTagInput{Tag: &tag}); err != nil {
								return err
							}
							ids = append(ids, tag.ID)
						}
					}
					return nil
				}))

				for _, tt := range []struct {
					name  string
					input string
					want  string
				}{
					{"omitted", "", `{"keep":%q,"changed":"old","remove":"old"}`},
					{"null", `null`, `{"keep":%q,"changed":"old","remove":"old"}`},
					{"empty_patch", `{"partial":{}}`, `{"keep":%q,"changed":"old","remove":"old"}`},
					{"set_and_remove", `{"partial":{"changed":2.5,"count":12,"text":"012","blank":"","__proto__":"literal"},"remove":["remove","absent"]}`, `{"keep":%q,"changed":2.5,"count":12,"text":"012","blank":"","__proto__":"literal"}`},
					{"remove_only", `{"remove":["remove"]}`, `{"keep":%q,"changed":"old"}`},
					{"clear_value_and_remove", `{"partial":{"changed":""},"remove":["remove"]}`, `{"keep":%q,"changed":""}`},
					{"remove_all_shared", `{"remove":["keep","changed","remove"]}`, `{}`},
					{"clear_all", `{"full":{}}`, `{}`},
				} {
					t.Run(tt.name, func(t *testing.T) {
						*updater = customFieldsBulkUpdater{}
						require.NoError(t, repository.WithTxn(testCtx, func(ctx context.Context) error {
							for i, id := range ids {
								fields := map[string]interface{}{
									"keep": names[i], "changed": "old", "remove": "old",
								}
								if i == 0 {
									fields["only_first"] = "private"
								}
								if err := store.SetCustomFields(ctx, id, models.CustomFieldsInput{Full: fields}); err != nil {
									return err
								}
							}
							return nil
						}))

						input := map[string]interface{}{"ids": []string{strconv.Itoa(ids[0]), strconv.Itoa(ids[1])}, "favorite": true}
						if tt.input != "" {
							input["custom_fields"] = json.RawMessage(tt.input)
						}
						mutation := "bulk" + entity + "Update"
						selection := "{ id }"
						if background {
							mutation += "Job"
							selection = ""
							input["ids"] = []string{}
							input["apply_to_items_matching_filters"] = true
							input["find_filter"] = map[string]interface{}{"q": "bulk custom target", "per_page": 1}
						}
						body, err := json.Marshal(map[string]interface{}{
							"query":     fmt.Sprintf("mutation($input: Bulk%sUpdateInput!) { result: %s(input: $input) %s }", entity, mutation, selection),
							"variables": map[string]interface{}{"input": input},
						})
						require.NoError(t, err)
						request := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
						request.Header.Set("Content-Type", "application/json")
						response := httptest.NewRecorder()
						server.ServeHTTP(response, request)
						require.Equal(t, http.StatusOK, response.Code)
						if background {
							require.JSONEq(t, `{"data":{"result":"1"}}`, response.Body.String())
							require.ElementsMatch(t, ids[:2], updater.ids)
							if tt.input != "" {
								require.Contains(t, updater.fields, "custom_fields")
							}
							for _, id := range updater.ids {
								require.NoError(t, repository.WithTxn(testCtx, func(ctx context.Context) error {
									return updater.operation.Update(ctx, id)
								}))
							}
						} else {
							require.JSONEq(t, fmt.Sprintf(`{"data":{"result":[{"id":"%d"},{"id":"%d"}]}}`, ids[0], ids[1]), response.Body.String())
							require.Nil(t, updater.operation)
						}

						require.NoError(t, repository.WithReadTxn(testCtx, func(ctx context.Context) error {
							for i, id := range ids {
								fields, err := store.GetCustomFields(ctx, id)
								if err != nil {
									return err
								}
								encoded, err := json.Marshal(fields)
								require.NoError(t, err)
								want := tt.want
								if i == 2 {
									want = `{"keep":%q,"changed":"old","remove":"old"}`
								}
								if want != `{}` {
									want = fmt.Sprintf(want, names[i])
								}
								var wantFields map[string]interface{}
								require.NoError(t, json.Unmarshal([]byte(want), &wantFields))
								// Even removing every shared field must preserve unshared data.
								// Full replacement remains available to existing API clients.
								if i == 0 && tt.name != "clear_all" {
									wantFields["only_first"] = "private"
								}
								wantJSON, err := json.Marshal(wantFields)
								require.NoError(t, err)
								require.JSONEq(t, string(wantJSON), string(encoded))
							}
							return nil
						}))
					})
				}
			})
		}
	}
}
