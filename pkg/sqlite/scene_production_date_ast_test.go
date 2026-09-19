//go:build integration

package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSceneProductionDateAST(t *testing.T) {
	runWithRollbackTxn(t, "production date AST and performer age", func(t *testing.T, ctx context.Context) {
		birthdate, err := models.ParseDate("1990-01-01")
		require.NoError(t, err)
		release, err := models.ParseDate("2020-01-01")
		require.NoError(t, err)
		production, err := models.ParseDate("2010-06-15")
		require.NoError(t, err)
		performer := models.NewPerformer()
		performer.Name, performer.Birthdate = "production AST performer", &birthdate
		require.NoError(t, db.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &performer}))
		prefix := "production AST scene"
		var ids []int
		for i, date := range []*models.Date{&production, nil} {
			scene := models.NewScene()
			scene.Title, scene.Date, scene.ProductionDate = prefix, &release, date
			scene.Organized = i == 0
			scene.PerformerIDs = models.NewRelatedIDs([]int{performer.ID})
			require.NoError(t, db.Scene.Create(ctx, &scene, nil))
			ids = append(ids, scene.ID)
		}
		for _, test := range []struct {
			name, field string
			value       interface{}
			want        int
		}{
			{"date", "production_date", models.DateCriterionInput{Value: "2010-06-15", Modifier: models.CriterionModifierEquals}, ids[0]},
			{"missing date", "production_date", models.DateCriterionInput{Modifier: models.CriterionModifierIsNull}, ids[1]},
			{"missing criterion", "is_missing", "production_date", ids[1]},
			{"age at production", "performer_age", models.IntCriterionInput{Value: 20, Modifier: models.CriterionModifierEquals}, ids[0]},
			{"age at release fallback", "performer_age", models.IntCriterionInput{Value: 30, Modifier: models.CriterionModifierEquals}, ids[1]},
		} {
			t.Run(test.name, func(t *testing.T) {
				ast := &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{Field: test.field, Value: test.value}}}
				scenes, count, err := db.Scene.QueryAST(ctx, ast, &models.FindFilterType{Q: &prefix})
				require.NoError(t, err)
				require.Equal(t, 1, count)
				require.Len(t, scenes, 1)
				require.Equal(t, test.want, scenes[0].ID)
			})
		}
		sort, direction := "performer_age", models.SortDirectionEnumAsc
		scenes, count, err := db.Scene.QueryAST(ctx, nil, &models.FindFilterType{Q: &prefix, Sort: &sort, Direction: &direction})
		require.NoError(t, err)
		require.Equal(t, 2, count)
		require.Equal(t, ids[0], scenes[0].ID)
		require.Equal(t, ids[1], scenes[1].ID)
	})
}
