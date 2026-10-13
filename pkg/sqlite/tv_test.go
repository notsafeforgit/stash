//go:build integration

package sqlite_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestTVMarkerFeed(t *testing.T) {
	runWithRollbackTxn(t, "TV markers and supplemental scenes", func(t *testing.T, ctx context.Context) {
		tag := models.NewTag()
		tag.Name = "TV-feed-test"
		require.NoError(t, db.Tag.Create(ctx, &models.CreateTagInput{Tag: &tag}))
		excludedTag := models.NewTag()
		excludedTag.Name = "TV excluded tag"
		require.NoError(t, db.Tag.Create(ctx, &models.CreateTagInput{Tag: &excludedTag}))
		newScene := func(title string) int {
			scene := models.NewScene()
			scene.Title = title
			require.NoError(t, db.Scene.Create(ctx, &scene, nil))
			return scene.ID
		}
		newMarker := func(scene int, title string, seconds float64) int {
			marker := models.NewSceneMarker()
			marker.SceneID, marker.PrimaryTagID = scene, tag.ID
			if title == "TV excluded marker" {
				marker.PrimaryTagID = excludedTag.ID
			}
			marker.Title, marker.Seconds = title, seconds
			require.NoError(t, db.SceneMarker.Create(ctx, &marker))
			return marker.ID
		}
		both := newScene("TV extra A both")
		extra := newScene("TV extra B whole scene")
		nonMatchingMarker := newScene("TV extra C unselected marker")
		outside := newScene("TV outside scene filter")
		newScene("TV neither filter")
		marker1 := newMarker(both, "ZZ TV chosen Z marker", 10)
		marker2 := newMarker(both, "ZZ TV chosen Y marker", 20)
		newMarker(nonMatchingMarker, "TV excluded marker", 5)
		markerOutside := newMarker(outside, "ZZ TV chosen X marker", 3)
		ast := func(value string) *models.FilterAST {
			return &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{
				Field: "title", Value: models.StringCriterionInput{Value: value, Modifier: models.CriterionModifierIncludes},
			}}}
		}
		markerAST := &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{
			Field: "tags", Value: models.HierarchicalMultiCriterionInput{Value: []string{strconv.Itoa(tag.ID)}, Modifier: models.CriterionModifierIncludes},
		}}}
		sceneAST := ast(`"TV extra"`)
		page, size, sort := 1, 2, "title"
		filter := &models.FindFilterType{Page: &page, PerPage: &size, Sort: &sort}
		var all []*models.TVFeedItem
		for ; page <= 3; page++ {
			items, total, err := db.SceneMarker.QueryTV(ctx, markerAST, sceneAST, filter, nil)
			require.NoError(t, err)
			require.Equal(t, 5, total)
			if page == 1 {
				// These scenes sort before every matching marker. The overlapping
				// scene must already be excluded from this first page.
				require.Len(t, items, 2)
				require.Equal(t, extra, items[0].Scene.ID)
				require.Equal(t, nonMatchingMarker, items[1].Scene.ID)
			}
			all = append(all, items...)
		}
		var wholeScenes, markerIDs []int
		for _, item := range all {
			if item.Marker == nil {
				wholeScenes = append(wholeScenes, item.Scene.ID)
			} else {
				require.Equal(t, item.Scene.ID, item.Marker.SceneID)
				markerIDs = append(markerIDs, item.Marker.ID)
			}
		}
		require.ElementsMatch(t, []int{extra, nonMatchingMarker}, wholeScenes)
		require.ElementsMatch(t, []int{marker1, marker2, markerOutside}, markerIDs)

		// Exclusion uses the matching marker search as well as its AST. A
		// scene whose markers fail that search is eligible for whole playback.
		search, sceneSearch := `"Z marker"`, "whole scene"
		filter.Q, filter.Page = &search, nil
		items, total, err := db.SceneMarker.QueryTV(ctx, markerAST, sceneAST, filter, &sceneSearch)
		require.NoError(t, err)
		require.Equal(t, 2, total)
		require.Len(t, items, 2)
		sceneSearch = "both"
		search = "no such marker"
		items, total, err = db.SceneMarker.QueryTV(ctx, markerAST, sceneAST, filter, &sceneSearch)
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Nil(t, items[0].Marker)
		require.Equal(t, both, items[0].Scene.ID)

		// Random is a single repeatable order across both kinds and all pages.
		filter.Q, filter.Page, filter.PerPage = nil, nil, nil
		sort = "random_23"
		first, total, err := db.SceneMarker.QueryTV(ctx, markerAST, sceneAST, filter, nil)
		require.NoError(t, err)
		require.Equal(t, 5, total)
		second, _, err := db.SceneMarker.QueryTV(ctx, markerAST, sceneAST, filter, nil)
		require.NoError(t, err)
		require.Equal(t, first, second)
		filter.Page, filter.PerPage = &page, &size
		all = nil
		for page = 1; page <= 3; page++ {
			items, _, err := db.SceneMarker.QueryTV(ctx, markerAST, sceneAST, filter, nil)
			require.NoError(t, err)
			all = append(all, items...)
		}
		require.Equal(t, first, all)
		for _, invalid := range []string{"random_bad", "title;DROP TABLE scenes", "not_a_sort"} {
			filter.Sort = &invalid
			_, _, err := db.SceneMarker.QueryTV(ctx, markerAST, sceneAST, filter, nil)
			require.Error(t, err)
		}
		filter.Sort = nil
		_, _, err = db.SceneMarker.QueryTV(ctx, markerAST, &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{Field: "unknown"}}}, filter, nil)
		require.Error(t, err)
	})
}
