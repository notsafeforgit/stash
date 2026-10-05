package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/stashapp/stash/pkg/group"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stretchr/testify/require"
)

func TestNativeGroupMutationsAndNotifications(t *testing.T) {
	repo := pluginNotificationRepository(t)
	hooks := &notificationRecorder{}
	updater := &customFieldsBulkUpdater{}
	resolver := &Resolver{repository: repo, hookExecutor: hooks, bulkUpdater: updater,
		groupService: &group.Service{Repository: repo.Group.(group.CreatorUpdater)}}
	call := nativeResolverCaller(t, resolver)
	var expectedName string
	var expectedHook hook.TriggerEnum
	hooks.check = func(_ context.Context, notification notificationCall) {
		require.Equal(t, expectedHook, notification.kind)
		// A fresh transaction must observe the committed edit, independently of
		// the mutation's transaction context.
		require.NoError(t, repo.WithReadTxn(testCtx, func(ctx context.Context) error {
			current, err := repo.Group.Find(ctx, notification.id)
			if expectedHook == hook.GroupDestroyPost {
				require.Nil(t, current)
			} else {
				require.NotNil(t, current)
				require.Equal(t, expectedName, current.Name)
			}
			return err
		}))
	}
	expectedName, expectedHook = "Native group", hook.GroupCreatePost
	created := call(`mutation { groupCreate(input:{name:"Native group",urls:["https://example.test/a","https://example.test/b"]}) { id name } }`, nil)
	require.NotContains(t, created, `"errors"`)
	var result struct {
		Data struct {
			GroupCreate struct{ ID string }
		}
	}
	require.NoError(t, json.Unmarshal([]byte(created), &result))
	id := result.Data.GroupCreate.ID
	require.NotEmpty(t, id)
	groupID, err := strconv.Atoi(id)
	require.NoError(t, err)
	require.Len(t, hooks.calls, 1)
	require.NoError(t, repo.WithReadTxn(testCtx, func(ctx context.Context) error {
		urls, err := repo.Group.GetURLs(ctx, groupID)
		require.Equal(t, []string{"https://example.test/a", "https://example.test/b"}, urls)
		return err
	}))

	hooks.calls = nil
	expectedName, expectedHook = "Renamed", hook.GroupUpdatePost
	require.NotContains(t, call(`mutation($input:GroupUpdateInput!){groupUpdate(input:$input){id name}}`, map[string]any{
		"input": map[string]any{"id": id, "name": expectedName},
	}), `"errors"`)
	require.Len(t, hooks.calls, 1)
	require.Contains(t, hooks.calls[0].fields, "name")

	hooks.calls = nil
	bulk := `mutation($input:BulkGroupUpdateInput!){bulkGroupUpdate(input:$input){status selected_count updated_ids}}`
	// A later invalid ID rolls back the entire explicit selection and emits no
	// hook, including the former movie hook that ran inside the transaction.
	require.Contains(t, call(bulk, map[string]any{"input": map[string]any{"ids": []string{id, "999999"}, "director": "Rolled back"}}), `"errors"`)
	require.Empty(t, hooks.calls)
	require.NoError(t, repo.WithReadTxn(testCtx, func(ctx context.Context) error {
		current, err := repo.Group.Find(ctx, groupID)
		require.Empty(t, current.Director)
		return err
	}))
	require.JSONEq(t, fmt.Sprintf(`{"data":{"bulkGroupUpdate":{"status":"COMPLETED","selected_count":1,"updated_ids":[%q]}}}`, id), call(bulk,
		map[string]any{"input": map[string]any{"ids": []string{id}, "director": "Committed"}}))
	require.Len(t, hooks.calls, 1)
	hooks.calls = nil
	require.NotContains(t, call(bulk, map[string]any{"input": map[string]any{
		"ids": []string{}, "director": "Queued", "apply_to_items_matching_filters": true,
		"find_filter": map[string]any{"q": expectedName},
	}}), `"errors"`)
	require.Equal(t, []int{groupID}, updater.ids)
	require.Empty(t, hooks.calls)
	require.NoError(t, repo.WithTxn(testCtx, func(ctx context.Context) error { return updater.operation.Update(ctx, groupID) }))
	require.Empty(t, hooks.calls, "the worker owns after-commit notifications; the operation emits none")
	require.NoError(t, repo.WithReadTxn(testCtx, func(ctx context.Context) error {
		current, err := repo.Group.Find(ctx, groupID)
		require.Equal(t, "Queued", current.Director)
		return err
	}))

	expectedHook = hook.GroupDestroyPost
	require.JSONEq(t, `{"data":{"groupDestroy":true}}`, call(`mutation($input:GroupDestroyInput!){groupDestroy(input:$input)}`, map[string]any{"input": map[string]any{"id": id}}))
	require.Len(t, hooks.calls, 1)
}

func TestNativeSceneGroupRelationshipAndRetiredAliases(t *testing.T) {
	repo := pluginNotificationRepository(t)
	hooks := &notificationRecorder{}
	call := nativeResolverCaller(t, &Resolver{repository: repo, hookExecutor: hooks})
	scene, g := models.NewScene(), models.NewGroup()
	g.Name = "Membership"
	require.NoError(t, repo.WithTxn(testCtx, func(ctx context.Context) error {
		if err := repo.Group.Create(ctx, &g); err != nil {
			return err
		}
		return repo.Scene.Create(ctx, &scene, nil)
	}))
	input := map[string]any{"id": strconv.Itoa(scene.ID), "groups": []any{map[string]any{"group_id": strconv.Itoa(g.ID), "scene_index": 3}}}
	require.NotContains(t, call(`mutation($input:SceneUpdateInput!){sceneUpdate(input:$input){id}}`, map[string]any{"input": input}), `"errors"`)
	require.Len(t, hooks.calls, 1)
	require.Contains(t, hooks.calls[0].fields, "groups")
	body, err := json.Marshal(hooks.calls[0].input)
	require.NoError(t, err)
	require.Contains(t, string(body), `"groups":[{"group_id":`)
	require.NotContains(t, string(body), `"movies"`)
	require.NoError(t, repo.WithReadTxn(testCtx, func(ctx context.Context) error {
		groups, err := repo.Scene.GetGroups(ctx, scene.ID)
		require.Equal(t, []models.GroupsScenes{{GroupID: g.ID, SceneIndex: PtrInt(3)}}, groups)
		return err
	}))
	for _, query := range []string{
		`{findMovie(id:"1"){id}}`, `{findMovies{count}}`, `{allMovies{id}}`,
		`mutation{movieCreate(input:{name:"Old"}){id}}`,
		`mutation{sceneUpdate(input:{id:"1",movies:[]}){id}}`,
		`mutation{bulkSceneUpdate(input:{ids:[],movie_ids:{ids:[],mode:SET}}){status}}`,
		`{findScene(id:"1"){movies{movie{id}}}}`, `{stats{movie_count}}`,
		`{scrapeMovieURL(url:"https://example.test"){name}}`,
		`{scrapeURL(url:"https://example.test",ty:MOVIE){__typename}}`,
		`mutation{exportObjects(input:{movies:{ids:["1"]}})}`,
	} {
		require.Contains(t, call(query, nil), `"errors"`, query)
	}
	for _, trigger := range []string{"Movie.Create.Post", "Movie.Update.Post", "Movie.Destroy.Post"} {
		require.False(t, hook.TriggerEnum(trigger).IsValid())
	}
}
