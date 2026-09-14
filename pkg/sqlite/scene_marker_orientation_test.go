//go:build integration

package sqlite_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMarkerQueryASTOrientation(t *testing.T) {
	runWithRollbackTxn(t, "marker orientation uses parent video dimensions", func(t *testing.T, ctx context.Context) {
		var ids []string
		markers := make(map[models.OrientationEnum]int)
		for _, dimensions := range []struct {
			orientation   models.OrientationEnum
			width, height int
		}{
			{models.OrientationPortrait, 480, 960},
			{models.OrientationLandscape, 960, 480},
			{models.OrientationSquare, 480, 480},
		} {
			scene, err := createScene(ctx, dimensions.width, dimensions.height)
			require.NoError(t, err)
			marker := models.NewSceneMarker()
			marker.SceneID, marker.PrimaryTagID = scene.ID, tagIDs[0]
			require.NoError(t, db.SceneMarker.Create(ctx, &marker))
			ids = append(ids, strconv.Itoa(scene.ID))
			markers[dimensions.orientation] = marker.ID
		}
		for _, orientation := range []models.OrientationEnum{models.OrientationPortrait, models.OrientationLandscape} {
			ast := &models.FilterAST{Root: &models.FilterASTNode{Group: &models.FilterASTGroup{
				Operator: models.FilterGroupOperatorAnd,
				Children: []*models.FilterASTNode{
					{Condition: &models.FilterASTCondition{Field: "scenes", Value: map[string]interface{}{"modifier": models.CriterionModifierIncludes, "value": ids}}},
					{Group: &models.FilterASTGroup{Operator: models.FilterGroupOperatorOr, Children: []*models.FilterASTNode{
						{Condition: &models.FilterASTCondition{Field: "orientation", Value: map[string]interface{}{"value": []models.OrientationEnum{orientation}}}},
						{Condition: &models.FilterASTCondition{Field: "orientation", Value: map[string]interface{}{"value": []models.OrientationEnum{models.OrientationSquare}}}},
					}}},
				},
			}}}
			result, count, err := db.SceneMarker.QueryAST(ctx, ast, nil)
			require.NoError(t, err)
			require.Equal(t, 2, count)
			require.ElementsMatch(t, []int{markers[orientation], markers[models.OrientationSquare]}, markersToIDs(result))
		}
	})
}
