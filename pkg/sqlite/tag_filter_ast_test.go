//go:build integration

package sqlite_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestTagFilterASTMatchAllIncludesExcludes(t *testing.T) {
	runWithRollbackTxn(t, "tag include and exclude truth table", func(t *testing.T, ctx context.Context) {
		createTag := func(name string) int {
			tag := models.NewTag()
			tag.Name = name
			require.NoError(t, db.Tag.Create(ctx, &models.CreateTagInput{Tag: &tag}))
			return tag.ID
		}
		included := createTag("AST include tag")
		excluded := createTag("AST exclude tag")
		neutral := createTag("AST neutral tag")
		descendant := createTag("AST excluded descendant")
		require.NoError(t, db.Tag.UpdateParentTags(ctx, descendant, []int{excluded}))

		var exactMarkers, descendantMarkers, exactScenes, descendantScenes []int
		for _, item := range []struct {
			name               string
			primaryTag         int
			additionalTags     []int
			matchesExact       bool
			matchesDescendants bool
		}{
			{"included primary", included, nil, true, true},
			{"included additional", neutral, []int{included}, true, true},
			{"excluded primary", excluded, []int{included}, false, false},
			{"excluded additional", included, []int{excluded}, false, false},
			{"excluded only", excluded, nil, false, false},
			{"neither tag", neutral, nil, false, false},
			{"descendant primary", descendant, []int{included}, true, false},
			{"descendant additional", included, []int{descendant}, true, false},
		} {
			scene := models.NewScene()
			scene.Title = item.name
			scene.TagIDs = models.NewRelatedIDs(append([]int{item.primaryTag}, item.additionalTags...))
			require.NoError(t, db.Scene.Create(ctx, &scene, nil))
			marker := models.NewSceneMarker()
			marker.Title, marker.SceneID, marker.PrimaryTagID = item.name, scene.ID, item.primaryTag
			require.NoError(t, db.SceneMarker.Create(ctx, &marker))
			require.NoError(t, db.SceneMarker.UpdateTags(ctx, marker.ID, item.additionalTags))
			if item.matchesExact {
				exactMarkers = append(exactMarkers, marker.ID)
				exactScenes = append(exactScenes, scene.ID)
			}
			if item.matchesDescendants {
				descendantMarkers = append(descendantMarkers, marker.ID)
				descendantScenes = append(descendantScenes, scene.ID)
			}
		}

		for _, target := range []string{"marker tags", "marker scene tags", "scene tags"} {
			for _, depth := range []int{0, 1} {
				for _, reverse := range []bool{false, true} {
					name := target + "/depth=" + strconv.Itoa(depth) + "/exclude-first=" + strconv.FormatBool(reverse)
					t.Run(name, func(t *testing.T) {
						field := "tags"
						if target == "marker scene tags" {
							field = "scene_tags"
						}
						conditions := []*models.FilterASTNode{
							{Condition: &models.FilterASTCondition{Field: field, Value: models.HierarchicalMultiCriterionInput{
								Value: []string{strconv.Itoa(included)}, Modifier: models.CriterionModifierIncludesAll,
							}}},
							{Condition: &models.FilterASTCondition{Field: field, Value: models.HierarchicalMultiCriterionInput{
								Value: []string{strconv.Itoa(excluded)}, Modifier: models.CriterionModifierExcludes, Depth: &depth,
							}}},
						}
						if reverse {
							conditions[0], conditions[1] = conditions[1], conditions[0]
						}
						ast := &models.FilterAST{Root: &models.FilterASTNode{Group: &models.FilterASTGroup{
							Operator: models.FilterGroupOperatorAnd, Children: conditions,
						}}}
						var ids []int
						var count int
						var err error
						expected := exactMarkers
						if depth != 0 {
							expected = descendantMarkers
						}
						if target == "scene tags" {
							var scenes []*models.Scene
							scenes, count, err = db.Scene.QueryAST(ctx, ast, nil)
							for _, scene := range scenes {
								ids = append(ids, scene.ID)
							}
							expected = exactScenes
							if depth != 0 {
								expected = descendantScenes
							}
						} else {
							var markers []*models.SceneMarker
							markers, count, err = db.SceneMarker.QueryAST(ctx, ast, nil)
							ids = markersToIDs(markers)
						}
						require.NoError(t, err)
						require.ElementsMatch(t, expected, ids)
						require.Equal(t, len(expected), count)
					})
				}
			}
		}
	})
}
