package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stretchr/testify/require"
)

func TestNativeBulkUpdateContract(t *testing.T) {
	repo := pluginNotificationRepository(t)
	hooks := &notificationRecorder{}
	updater := &customFieldsBulkUpdater{}
	server := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{repository: repo, hookExecutor: hooks, bulkUpdater: updater}}))
	server.AddTransport(transport.POST{})
	call := func(query string, input any) string {
		t.Helper()
		body, err := json.Marshal(map[string]any{"query": query, "variables": map[string]any{"input": input}})
		require.NoError(t, err)
		request := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response.Body.String()
	}

	for _, entity := range []string{"Scene", "SceneMarker", "Image", "Gallery", "Performer", "Studio", "Tag", "Group"} {
		t.Run(entity, func(t *testing.T) {
			query := fmt.Sprintf(`mutation($input: Bulk%sUpdateInput!) { result: bulk%sUpdate(input: $input) { status job_id selected_count updated_ids } }`, entity, entity)
			require.JSONEq(t, `{"data":{"result":{"status":"COMPLETED","job_id":null,"selected_count":0,"updated_ids":[]}}}`, call(query, map[string]any{"ids": []string{}}))
			require.Empty(t, hooks.calls)
			require.Nil(t, updater.operation)
			removed := fmt.Sprintf(`mutation { bulk%sUpdateJob(input: {ids: []}) }`, entity)
			require.Contains(t, call(removed, nil), `Cannot query field`)
		})
	}
	for _, field := range []string{"bulkMovieUpdate", "bulkMovieUpdateJob"} {
		require.Contains(t, call(fmt.Sprintf(`mutation { %s(input: {ids: []}) { __typename } }`, field), nil), `Cannot query field`)
	}

	scene := models.NewScene()
	scene.Title = "Original"
	require.NoError(t, repo.WithTxn(testCtx, func(ctx context.Context) error { return repo.Scene.Create(ctx, &scene, nil) }))
	id := strconv.Itoa(scene.ID)
	query := `mutation($input: BulkSceneUpdateInput!) { result: bulkSceneUpdate(input: $input) { status job_id selected_count updated_ids } }`
	readTitle := func() string {
		t.Helper()
		var title string
		require.NoError(t, repo.WithReadTxn(testCtx, func(ctx context.Context) error {
			current, err := repo.Scene.Find(ctx, scene.ID)
			if err == nil {
				title = current.Title
			}
			return err
		}))
		return title
	}

	// A later missing entity must roll back the earlier successful edit and emit
	// no success acknowledgment or notifications.
	result := call(query, map[string]any{"ids": []string{id, "999999"}, "title": "Rolled back"})
	require.Contains(t, result, `"errors"`)
	require.NotContains(t, result, `COMPLETED`)
	require.Equal(t, "Original", readTitle())
	require.Empty(t, hooks.calls)

	hooks.check = func(_ context.Context, call notificationCall) {
		require.Equal(t, hook.SceneUpdatePost, call.kind)
		require.Equal(t, "Committed", readTitle())
	}
	require.JSONEq(t, fmt.Sprintf(`{"data":{"result":{"status":"COMPLETED","job_id":null,"selected_count":1,"updated_ids":[%q]}}}`, id), call(query, map[string]any{"ids": []string{id}, "title": "Committed"}))
	require.Len(t, hooks.calls, 1)
	hooks.calls = nil

	// Admission snapshots all matches, regardless of UI pagination. It neither
	// edits the selected entity nor claims an updated ID before worker execution.
	require.JSONEq(t, `{"data":{"result":{"status":"QUEUED","job_id":"1","selected_count":1,"updated_ids":[]}}}`, call(query, map[string]any{
		"ids": []string{}, "title": "Queued", "apply_to_items_matching_filters": true,
		"find_filter": map[string]any{"q": "Committed", "page": 99, "per_page": 1},
	}))
	require.Equal(t, []int{scene.ID}, updater.ids)
	require.Equal(t, "Committed", readTitle())
	require.Empty(t, hooks.calls)
}
