//go:build integration

package sqlite_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMediaQueryASTStudioTags(t *testing.T) {
	runWithRollbackTxn(t, "studio tags across media", func(t *testing.T, ctx context.Context) {
		createTag := func(name string) int {
			tag := models.NewTag()
			tag.Name = name
			require.NoError(t, db.Tag.Create(ctx, &models.CreateTagInput{Tag: &tag}))
			return tag.ID
		}
		rootTag := createTag("Studio AST root tag")
		childTag := createTag("Studio AST child tag")
		parentTag := createTag("Studio AST parent studio tag")
		otherTag := createTag("Studio AST unrelated tag")
		require.NoError(t, db.Tag.UpdateParentTags(ctx, childTag, []int{rootTag}))
		createStudio := func(name string, parent *int, tags ...int) int {
			studio := models.NewStudio()
			studio.Name, studio.ParentID = name, parent
			studio.TagIDs = models.NewRelatedIDs(tags)
			require.NoError(t, db.Studio.Create(ctx, &models.CreateStudioInput{Studio: &studio}))
			return studio.ID
		}
		parent := createStudio("Studio AST parent", nil, parentTag)
		child := createStudio("Studio AST child", &parent, childTag)
		grandchild := createStudio("Studio AST grandchild", &child)
		other := createStudio("Studio AST other", nil, otherTag)
		untagged := createStudio("Studio AST untagged", nil)

		// Scope every query to these records, including the negative criteria,
		// so unrelated shared fixtures cannot hide missing-studio regressions.
		const prefix = "Studio tags AST / "
		galleryIDs, imageIDs, sceneIDs := map[string]int{}, map[string]int{}, map[string]int{}
		for _, item := range []struct {
			name   string
			studio *int
		}{
			{"parent", &parent}, {"child", &child}, {"grandchild", &grandchild},
			{"other", &other}, {"untagged", &untagged}, {"unassigned", nil},
		} {
			gallery := models.NewGallery()
			gallery.Title, gallery.StudioID = prefix+item.name, item.studio
			require.NoError(t, db.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &gallery}))
			galleryIDs[item.name] = gallery.ID
			image := models.NewImage()
			image.Title, image.StudioID = gallery.Title, item.studio
			require.NoError(t, db.Image.Create(ctx, &models.CreateImageInput{Image: &image}))
			imageIDs[item.name] = image.ID
			scene := models.NewScene()
			scene.Title, scene.StudioID = gallery.Title, item.studio
			require.NoError(t, db.Scene.Create(ctx, &scene, nil))
			sceneIDs[item.name] = scene.ID
		}

		for _, tc := range []struct {
			name     string
			mode     models.StudioTagHierarchyMode
			modifier models.CriterionModifier
			tags     []int
			excludes []int
			depth    int
			want     []string
		}{
			{name: "default studio only", modifier: models.CriterionModifierIncludes, tags: []int{childTag}, want: []string{"child"}},
			{name: "exact studio", mode: models.StudioTagHierarchyModeExact, modifier: models.CriterionModifierIncludes, tags: []int{childTag}, want: []string{"child"}},
			{name: "ancestor studios", mode: models.StudioTagHierarchyModeAncestors, modifier: models.CriterionModifierIncludes, tags: []int{childTag}, want: []string{"child", "grandchild"}},
			{name: "descendant studios", mode: models.StudioTagHierarchyModeDescendants, modifier: models.CriterionModifierIncludes, tags: []int{childTag}, want: []string{"parent", "child"}},
			{name: "both studio directions", mode: models.StudioTagHierarchyModeAncestorsDescendants, modifier: models.CriterionModifierIncludes, tags: []int{childTag}, want: []string{"parent", "child", "grandchild"}},
			{name: "exact tag only", modifier: models.CriterionModifierIncludes, tags: []int{rootTag}},
			{name: "descendant tags", modifier: models.CriterionModifierIncludes, tags: []int{rootTag}, depth: -1, want: []string{"child"}},
			{name: "all selected tags", modifier: models.CriterionModifierIncludesAll, tags: []int{childTag, otherTag}},
			{name: "excluded descendant tags", modifier: models.CriterionModifierExcludes, tags: []int{rootTag}, depth: -1, want: []string{"parent", "grandchild", "other", "untagged", "unassigned"}},
			{name: "excluded studio hierarchy", mode: models.StudioTagHierarchyModeAncestorsDescendants, modifier: models.CriterionModifierExcludes, tags: []int{childTag}, want: []string{"other", "untagged", "unassigned"}},
			{name: "exclude-only selection", mode: models.StudioTagHierarchyModeAncestorsDescendants, modifier: models.CriterionModifierIncludesAll, excludes: []int{childTag}, want: []string{"other", "untagged", "unassigned"}},
			{name: "include and exclude", modifier: models.CriterionModifierIncludes, tags: []int{childTag, otherTag}, excludes: []int{otherTag}, want: []string{"child"}},
			{name: "no studio tags", modifier: models.CriterionModifierIsNull, want: []string{"grandchild", "untagged", "unassigned"}},
			{name: "no tags in studio hierarchy", mode: models.StudioTagHierarchyModeAncestorsDescendants, modifier: models.CriterionModifierIsNull, want: []string{"untagged", "unassigned"}},
			{name: "has studio tags", modifier: models.CriterionModifierNotNull, want: []string{"parent", "child", "other"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				input := models.StudioTagFilterInput{
					HierarchyMode: tc.mode,
					HierarchicalMultiCriterionInput: models.HierarchicalMultiCriterionInput{
						Modifier: tc.modifier,
						Depth:    &tc.depth,
					},
				}
				for _, id := range tc.tags {
					input.Value = append(input.Value, strconv.Itoa(id))
				}
				for _, id := range tc.excludes {
					input.Excludes = append(input.Excludes, strconv.Itoa(id))
				}
				ast := &models.FilterAST{Root: &models.FilterASTNode{Group: &models.FilterASTGroup{
					Operator: models.FilterGroupOperatorAnd,
					Children: []*models.FilterASTNode{
						{Condition: &models.FilterASTCondition{Field: "title", Value: models.StringCriterionInput{Modifier: models.CriterionModifierIncludes, Value: prefix}}},
						{Condition: &models.FilterASTCondition{Field: "studio_tags", Value: input}},
					},
				}}}
				check := func(t *testing.T, ids []int, count int, err error, fixtures map[string]int) {
					t.Helper()
					require.NoError(t, err)
					var expected []int
					for _, name := range tc.want {
						expected = append(expected, fixtures[name])
					}
					require.ElementsMatch(t, expected, ids)
					require.Equal(t, len(expected), count)
				}
				t.Run("galleries", func(t *testing.T) {
					rows, count, err := db.Gallery.QueryAST(ctx, ast, nil)
					var ids []int
					for _, row := range rows {
						ids = append(ids, row.ID)
					}
					check(t, ids, count, err, galleryIDs)
				})
				t.Run("images", func(t *testing.T) {
					rows, count, err := db.Image.QueryAST(ctx, ast, nil)
					var ids []int
					for _, row := range rows {
						ids = append(ids, row.ID)
					}
					check(t, ids, count, err, imageIDs)
				})
				t.Run("scenes", func(t *testing.T) {
					rows, count, err := db.Scene.QueryAST(ctx, ast, nil)
					var ids []int
					for _, row := range rows {
						ids = append(ids, row.ID)
					}
					check(t, ids, count, err, sceneIDs)
				})
			})
		}
	})
}
