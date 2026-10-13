package api

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestTVMarkerFeedUUIDBoundary(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "tv.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	scene, tag, marker := models.NewScene(), models.NewTag(), models.NewSceneMarker()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		tag.Name = "TV tag"
		require.NoError(t, repo.Tag.Create(ctx, &models.CreateTagInput{Tag: &tag}))
		scene.TagIDs = models.NewRelatedIDs([]int{tag.ID})
		require.NoError(t, repo.Scene.Create(ctx, &scene, nil))
		marker.SceneID, marker.PrimaryTagID, marker.Title = scene.ID, tag.ID, "TV marker"
		return repo.SceneMarker.Create(ctx, &marker)
	}))
	sceneID := libraryTestUUID(t, repo, models.ArchiveScene, scene.ID)
	markerID := libraryTestUUID(t, repo, models.ArchiveSceneMarker, marker.ID)
	tagID := libraryTestUUID(t, repo, models.ArchiveTag, tag.ID)
	call := nativeResolverCaller(t, &Resolver{repository: repo})
	criterion := func(field, id string) map[string]any {
		return map[string]any{"root": map[string]any{"condition": map[string]any{
			"field": field, "value": map[string]any{"value": []string{id}, "modifier": "INCLUDES"},
		}}}
	}
	query := `query($marker:FilterASTInput!,$scene:FilterASTInput!){findTVMarkerFeed(marker_filter_ast:$marker,scene_filter:{filter_ast:$scene}){count items{scene{id} marker{id}}}}`
	variables := map[string]any{"marker": criterion("tags", tagID), "scene": criterion("tags", tagID)}
	response := call(query, variables)
	require.JSONEq(t, `{"data":{"findTVMarkerFeed":{"count":1,"items":[{"scene":{"id":"`+sceneID+`"},"marker":{"id":"`+markerID+`"}}]}}}`, response)
	variables["marker"] = criterion("tags", "00000000-0000-4000-8000-000000000001")
	response = call(query, variables)
	require.JSONEq(t, `{"data":{"findTVMarkerFeed":{"count":1,"items":[{"scene":{"id":"`+sceneID+`"},"marker":null}]}}}`, response)
	variables["scene"] = criterion("tags", markerID)
	require.Contains(t, call(query, variables), "wrong kind")
	variables["scene"] = criterion("tags", "1")
	require.Contains(t, call(query, variables), `"errors"`)
}
