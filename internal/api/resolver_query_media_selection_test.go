package api

import (
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"
)

func TestMediaListSelectsCountIndependentlyOfRows(t *testing.T) {
	for _, useAST := range []bool{false, true} {
		for _, countOnly := range []bool{false, true} {
			db := mocks.NewDatabase()
			r, _ := newResolver(db)
			var filterAST *models.FilterAST
			if useAST {
				filterAST = &models.FilterAST{Root: &models.FilterASTNode{}}
			}
			find := &models.FindFilterType{Q: PtrString("needle")}
			for _, entity := range []string{"scenes", "images"} {
				field := entity
				if countOnly {
					field = "count"
				}
				ctx := withGqlContext(testCtx, nil)
				graphql.GetFieldContext(ctx).Field.Selections = ast.SelectionSet{&ast.Field{Name: field}}
				if entity == "scenes" {
					result := models.NewSceneQueryResult(db.Scene)
					result.Count = 500
					if !countOnly {
						result.IDs = []int{7}
						db.Scene.On("FindMany", mock.Anything, []int{7}).Return([]*models.Scene{{ID: 7}}, nil).Once()
					}
					db.Scene.On("Query", mock.Anything, models.SceneQueryOptions{
						QueryOptions:   models.QueryOptions{FindFilter: find, Count: countOnly},
						SceneFilterAST: filterAST, SkipItems: countOnly,
					}).Return(result, nil).Once()
					got, err := r.Query().FindScenes(ctx, nil, filterAST, nil, nil, find)
					require.NoError(t, err)
					if countOnly {
						require.Equal(t, 500, got.Count)
						require.Empty(t, got.Scenes)
					} else {
						require.Len(t, got.Scenes, 1)
					}
				} else {
					result := models.NewImageQueryResult(db.Image)
					result.Count = 500
					if !countOnly {
						result.IDs = []int{7}
						db.Image.On("FindMany", mock.Anything, []int{7}).Return([]*models.Image{{ID: 7}}, nil).Once()
					}
					db.Image.On("Query", mock.Anything, models.ImageQueryOptions{
						QueryOptions:   models.QueryOptions{FindFilter: find, Count: countOnly},
						ImageFilterAST: filterAST, SkipItems: countOnly,
					}).Return(result, nil).Once()
					got, err := r.Query().FindImages(ctx, nil, filterAST, nil, nil, find)
					require.NoError(t, err)
					if countOnly {
						require.Equal(t, 500, got.Count)
						require.Empty(t, got.Images)
					} else {
						require.Len(t, got.Images, 1)
					}
				}
			}
			db.AssertExpectations(t)
		}
	}
}
