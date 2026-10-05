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

func nativeQueryCaller(t *testing.T, repo models.Repository) func(string, map[string]any) string {
	t.Helper()
	server := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{repository: repo}}))
	server.AddTransport(transport.POST{})
	return func(query string, variables map[string]any) string {
		t.Helper()
		body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
		require.NoError(t, err)
		request := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response.Body.String()
	}
}

func nativeTitleAST(value string) map[string]any {
	return map[string]any{"root": map[string]any{"condition": map[string]any{
		"field": "title", "value": map[string]any{"value": value, "modifier": "INCLUDES"},
	}}}
}

func TestNativeEntityQueryContract(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "queries.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	call := nativeQueryCaller(t, repo)
	var markerSceneID, markerTagID int

	for _, tc := range []struct {
		entity, argument, rows, criterion, legacyIDs string
		mode                                         models.FilterMode
	}{
		{"Scenes", "scene", "scenes", "title", "scene_ids", models.FilterModeScenes},
		{"Images", "image", "images", "title", "image_ids", models.FilterModeImages},
		{"Performers", "performer", "performers", "name", "performer_ids", models.FilterModePerformers},
		{"Studios", "studio", "studios", "name", "", models.FilterModeStudios},
		{"Galleries", "gallery", "galleries", "title", "", models.FilterModeGalleries},
		{"Groups", "group", "groups", "name", "", models.FilterModeGroups},
		{"Tags", "tag", "tags", "name", "", models.FilterModeTags},
		{"SceneMarkers", "scene_marker", "scene_markers", "duration", "", models.FilterModeSceneMarkers},
	} {
		t.Run(tc.entity, func(t *testing.T) {
			var ids []int
			require.NoError(t, repo.WithTxn(testCtx, func(ctx context.Context) error {
				for index, name := range []string{"target first", "target second", "outside"} {
					var id int
					var err error
					if tc.mode == models.FilterModeSceneMarkers {
						marker := models.NewSceneMarker()
						marker.Title, marker.SceneID, marker.PrimaryTagID = name, markerSceneID, markerTagID
						end := float64(index + 1)
						marker.EndSeconds = &end
						err = repo.SceneMarker.Create(ctx, &marker)
						id = marker.ID
					} else {
						id, _, err = createBulkCustomFieldFixture(ctx, db, tc.mode, name)
					}
					if err != nil {
						return err
					}
					ids = append(ids, id)
				}
				return nil
			}))
			if tc.mode == models.FilterModeScenes {
				markerSceneID = ids[2]
			}
			if tc.mode == models.FilterModeTags {
				markerTagID = ids[2]
			}
			query := fmt.Sprintf(`query($ast: FilterASTInput, $filter: FindFilterType, $ids: [ID!]) {
				result: find%s(%s_filter_ast: $ast, filter: $filter, ids: $ids) { count rows: %s { id } }
			}`, tc.entity, tc.argument, tc.rows)
			condition := func(value string) map[string]any {
				if tc.criterion == "duration" {
					seconds := 1
					if value == "second" {
						seconds = 2
					}
					return map[string]any{"condition": map[string]any{"field": tc.criterion, "value": map[string]any{"value": seconds, "modifier": "EQUALS"}}}
				}
				return map[string]any{"condition": map[string]any{"field": tc.criterion, "value": map[string]any{"value": value, "modifier": "INCLUDES"}}}
			}
			ast := map[string]any{"root": map[string]any{"group": map[string]any{
				"operator": "OR", "children": []any{condition("first"), condition("second")},
			}}}
			variables := map[string]any{"ast": ast, "filter": map[string]any{"sort": "id", "direction": "ASC", "page": 2, "per_page": 1}}
			require.JSONEq(t, fmt.Sprintf(`{"data":{"result":{"count":2,"rows":[{"id":%q}]}}}`, strconv.Itoa(ids[1])), call(query, variables))
			variables["filter"] = map[string]any{"q": "first"}
			require.JSONEq(t, fmt.Sprintf(`{"data":{"result":{"count":1,"rows":[{"id":%q}]}}}`, strconv.Itoa(ids[0])), call(query, variables))
			require.JSONEq(t, fmt.Sprintf(`{"data":{"result":{"count":1,"rows":[{"id":%q}]}}}`, strconv.Itoa(ids[2])), call(query, map[string]any{"ids": []string{strconv.Itoa(ids[2])}}))
			require.Contains(t, call(query, map[string]any{"ids": []string{"invalid"}}), `"errors"`)
			require.JSONEq(t, `{"data":{"result":{"count":3}}}`, call(fmt.Sprintf(`{ result: find%s { count } }`, tc.entity), nil))
			require.Contains(t, call(fmt.Sprintf(`{ find%s(%s_filter: {}) { count } }`, tc.entity, tc.argument), nil), `Unknown argument`)
			if tc.legacyIDs != "" {
				require.Contains(t, call(fmt.Sprintf(`{ find%s(%s: [1]) { count } }`, tc.entity, tc.legacyIDs), nil), `Unknown argument`)
			}
		})
	}
}

func TestNativeMediaQueryAggregatesAndDuplicates(t *testing.T) {
	repo := pluginNotificationRepository(t)
	call := nativeQueryCaller(t, repo)
	var sceneIDs, imageIDs []int
	gallery := models.NewGallery()
	require.NoError(t, repo.WithTxn(testCtx, func(ctx context.Context) error {
		if err := repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &gallery}); err != nil {
			return err
		}
		folder := &models.Folder{Path: t.TempDir()}
		if err := repo.Folder.Create(ctx, folder); err != nil {
			return err
		}
		for index, name := range []string{"target first", "target second", "outside"} {
			factor := 1 << index
			for _, kind := range []string{"scene", "image"} {
				base := &models.BaseFile{
					ParentFolderID: folder.ID, Basename: fmt.Sprintf("%s-%d", kind, index), Size: int64(100 * factor),
					Fingerprints: models.Fingerprints{{Type: models.FingerprintTypePhash, Fingerprint: int64(0x1111111111111111)}},
				}
				if kind == "scene" {
					file := &models.VideoFile{BaseFile: base, Duration: float64(10 * factor), Width: 1000, Height: 1000}
					if err := repo.File.Create(ctx, file); err != nil {
						return err
					}
					scene := models.NewScene()
					scene.Title = name
					if err := repo.Scene.Create(ctx, &scene, []models.FileID{file.ID}); err != nil {
						return err
					}
					sceneIDs = append(sceneIDs, scene.ID)
				} else {
					file := &models.ImageFile{BaseFile: base, Width: 1000 * factor, Height: 1500}
					if err := repo.File.Create(ctx, file); err != nil {
						return err
					}
					image := models.NewImage()
					image.Title = name
					if index < 2 {
						image.GalleryIDs = models.NewRelatedIDs([]int{gallery.ID})
					}
					if err := repo.Image.Create(ctx, &models.CreateImageInput{Image: &image, FileIDs: []models.FileID{file.ID}}); err != nil {
						return err
					}
					imageIDs = append(imageIDs, image.ID)
				}
			}
		}
		return nil
	}))
	// The gallery viewer uses this relationship criterion rather than a flat
	// image filter. It must not admit an image outside the chosen gallery.
	require.JSONEq(t, fmt.Sprintf(`{"data":{"findImages":{"count":2,"images":[{"id":%q},{"id":%q}]}}}`, strconv.Itoa(imageIDs[0]), strconv.Itoa(imageIDs[1])), call(
		`query($ast: FilterASTInput!) { findImages(image_filter_ast: $ast, filter: {sort:"id",direction:ASC}) { count images { id } } }`,
		map[string]any{"ast": map[string]any{"root": map[string]any{"condition": map[string]any{
			"field": "galleries", "value": map[string]any{"modifier": "INCLUDES", "value": []string{strconv.Itoa(gallery.ID)}},
		}}}},
	))
	for _, tc := range []struct {
		entity, plural, aggregate string
		value                     float64
		ids                       []int
	}{
		{"Scene", "scenes", "duration", 30, sceneIDs},
		{"Image", "images", "megapixels", 4.5, imageIDs},
	} {
		t.Run(tc.entity, func(t *testing.T) {
			variables := map[string]any{"ast": nativeTitleAST("target")}
			for _, withRows := range []bool{false, true} {
				rowsQuery, rowsJSON := "", ""
				if withRows {
					rowsQuery = fmt.Sprintf("rows: %s { id }", tc.plural)
					rowsJSON = fmt.Sprintf(`,"rows":[{"id":%q}]`, strconv.Itoa(tc.ids[0]))
				}
				// Totals describe every match, even when rows are omitted or paginated.
				query := fmt.Sprintf(`query($ast: FilterASTInput!) { result: find%ss(%s_filter_ast: $ast, filter: {sort:"id",direction:ASC,per_page:1}) { count filesize %s %s } }`, tc.entity, tc.plural[:len(tc.plural)-1], tc.aggregate, rowsQuery)
				require.JSONEq(t, fmt.Sprintf(`{"data":{"result":{"count":2,"filesize":300,%q:%v%s}}}`, tc.aggregate, tc.value, rowsJSON), call(query, variables))
			}
			for _, groupQuery := range []bool{false, true} {
				field, selection := "findDuplicate"+tc.entity+"s", "{ id }"
				if groupQuery {
					field, selection = "findDuplicate"+tc.entity+"Groups", "{ count groups { id } }"
				}
				query := fmt.Sprintf(`query($ast: FilterASTInput!, $mode: DuplicateFilterMode!) { result: %s(%s_filter_ast: $ast, filter_mode: $mode) %s }`, field, tc.plural[:len(tc.plural)-1], selection)
				for _, mode := range []string{"ALL", "ANY"} {
					variables["mode"] = mode
					var ids []map[string]string
					limit := 2
					if mode == "ANY" {
						limit = 3
					}
					for _, id := range tc.ids[:limit] {
						ids = append(ids, map[string]string{"id": strconv.Itoa(id)})
					}
					var expected any = [][]map[string]string{ids}
					if groupQuery {
						expected = map[string]any{"count": 1, "groups": expected}
					}
					body, err := json.Marshal(map[string]any{"data": map[string]any{"result": expected}})
					require.NoError(t, err)
					require.JSONEq(t, string(body), call(query, variables))
				}
				require.Contains(t, call(fmt.Sprintf(`{ %s(%s_filter: {}) %s }`, field, tc.plural[:len(tc.plural)-1], selection), nil), `Unknown argument`)
			}
		})
	}
}
