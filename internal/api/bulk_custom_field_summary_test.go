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

func createBulkCustomFieldFixture(ctx context.Context, db *sqlite.Database, mode models.FilterMode, name string) (int, models.CustomFieldsWriter, error) {
	switch mode {
	case models.FilterModeScenes:
		v := models.NewScene()
		v.Title = name
		err := db.Scene.Create(ctx, &v, nil)
		return v.ID, db.Scene, err
	case models.FilterModeImages:
		v := models.NewImage()
		v.Title = name
		err := db.Image.Create(ctx, &models.CreateImageInput{Image: &v})
		return v.ID, db.Image, err
	case models.FilterModeGalleries:
		v := models.NewGallery()
		v.Title = name
		err := db.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &v})
		return v.ID, db.Gallery, err
	case models.FilterModePerformers:
		v := models.NewPerformer()
		v.Name = name
		err := db.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &v})
		return v.ID, db.Performer, err
	case models.FilterModeStudios:
		v := models.NewCreateStudioInput()
		v.Name = name
		err := db.Studio.Create(ctx, &v)
		return v.ID, db.Studio, err
	case models.FilterModeGroups:
		v := models.NewGroup()
		v.Name = name
		err := db.Group.Create(ctx, &v)
		return v.ID, db.Group, err
	case models.FilterModeTags:
		v := models.NewTag()
		v.Name = name
		err := db.Tag.Create(ctx, &models.CreateTagInput{Tag: &v})
		return v.ID, db.Tag, err
	default:
		return 0, nil, fmt.Errorf("unsupported fixture mode %s", mode)
	}
}

func TestBulkCustomFieldSummary(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	for _, mode := range []models.FilterMode{
		models.FilterModeScenes, models.FilterModeImages, models.FilterModeGalleries,
		models.FilterModePerformers, models.FilterModeStudios, models.FilterModeGroups, models.FilterModeTags,
	} {
		t.Run(string(mode), func(t *testing.T) {
			db := sqlite.NewDatabase()
			require.NoError(t, db.Open(filepath.Join(t.TempDir(), "stash.sqlite")))
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			repository := db.Repository()
			resolver := &Resolver{repository: repository}
			server := handler.New(NewExecutableSchema(Config{Resolvers: resolver}))
			server.AddTransport(transport.POST{})

			// Exceed the default query page; tags also cross the field-read batch boundary.
			count := 32
			if mode == models.FilterModeTags {
				count = 502
			}
			var ids []string
			require.NoError(t, repository.WithTxn(testCtx, func(ctx context.Context) error {
				for i := range count + 1 {
					name := fmt.Sprintf("target %03d", i)
					if i == count {
						name = "outside selection"
					}
					id, writer, err := createBulkCustomFieldFixture(ctx, db, mode, name)
					if err != nil {
						return err
					}
					ids = append(ids, strconv.Itoa(id))
					fields := map[string]interface{}{"same": "x", "mixed": i, "blank": "", "null": nil}
					if i < count-1 {
						fields["missing_on_last"] = "x"
					}
					if i == 0 {
						fields["first_only"] = "x"
					}
					if i == count {
						fields = map[string]interface{}{"outside_only": "x"}
					}
					if err := writer.SetCustomFields(ctx, id, models.CustomFieldsInput{Full: fields}); err != nil {
						return err
					}
				}
				return nil
			}))

			query := func(input map[string]interface{}) (*BulkCustomFieldSummary, json.RawMessage) {
				t.Helper()
				input["mode"] = mode
				body, err := json.Marshal(map[string]interface{}{
					"query":     `query($input: BulkCustomFieldTargetInput!) { summary: bulkCustomFieldSummary(input: $input) { count shared_names partial_names } }`,
					"variables": map[string]interface{}{"input": input},
				})
				require.NoError(t, err)
				request := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				server.ServeHTTP(response, request)
				require.Equal(t, http.StatusOK, response.Code)
				var result struct {
					Data   struct{ Summary *BulkCustomFieldSummary }
					Errors json.RawMessage
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
				return result.Data.Summary, result.Errors
			}
			field := "name"
			if mode == models.FilterModeScenes || mode == models.FilterModeImages || mode == models.FilterModeGalleries {
				field = "title"
			}
			ast, err := models.FilterASTFromObjectFilter(map[string]interface{}{
				field: map[string]interface{}{"value": "target", "modifier": "INCLUDES"},
			})
			require.NoError(t, err)
			for _, tt := range []struct {
				name  string
				input map[string]interface{}
				want  BulkCustomFieldSummary
			}{
				{"selected", map[string]interface{}{"ids": ids[:2]}, BulkCustomFieldSummary{Count: 2, SharedNames: []string{"blank", "missing_on_last", "mixed", "same"}, PartialNames: []string{"first_only"}}},
				{"duplicate_ids", map[string]interface{}{"ids": []string{ids[0], ids[0]}}, BulkCustomFieldSummary{Count: 1, SharedNames: []string{"blank", "first_only", "missing_on_last", "mixed", "same"}, PartialNames: []string{}}},
				{"empty_selection", map[string]interface{}{"ids": []string{}}, BulkCustomFieldSummary{Count: 0, SharedNames: []string{}, PartialNames: []string{}}},
				{"all_pages", map[string]interface{}{"find_filter": map[string]interface{}{"q": "target", "page": 5, "per_page": 1}}, BulkCustomFieldSummary{Count: count, SharedNames: []string{"blank", "mixed", "same"}, PartialNames: []string{"first_only", "missing_on_last"}}},
				{"ast_without_find_filter", map[string]interface{}{"filter_ast": ast}, BulkCustomFieldSummary{Count: count, SharedNames: []string{"blank", "mixed", "same"}, PartialNames: []string{"first_only", "missing_on_last"}}},
				{"no_matches", map[string]interface{}{"find_filter": map[string]interface{}{"q": "missing item"}}, BulkCustomFieldSummary{Count: 0, SharedNames: []string{}, PartialNames: []string{}}},
			} {
				t.Run(tt.name, func(t *testing.T) {
					got, errs := query(tt.input)
					require.Empty(t, errs, string(errs))
					require.Equal(t, &tt.want, got)
				})
			}
			for _, input := range []map[string]interface{}{
				{}, {"ids": []string{"999999"}}, {"ids": []string{"invalid"}},
				{"ids": ids[:1], "find_filter": map[string]interface{}{"q": "target"}},
			} {
				_, errs := query(input)
				require.NotEmpty(t, errs)
			}
		})
	}
}
