package api

import (
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestBulkSceneUpdate_ApplyToAll(t *testing.T) {
	db := mocks.NewDatabase()
	r, bulkUpdater := newResolver(db)

	sceneFilter := &models.SceneFilterType{
		Rating100: &models.IntCriterionInput{
			Value:    50,
			Modifier: models.CriterionModifierGreaterThan,
		},
	}
	findFilter := &models.FindFilterType{
		Sort:      PtrString("title"),
		Direction: PtrSortDirectionEnum(models.SortDirectionEnumAsc),
	}

	// Mock the query call that fetches all IDs
	// The resolver clears pagination and uses PerPageAll
	expectedFindFilter := *findFilter
	expectedFindFilter.Page = nil
	perPageAll := models.PerPageAll
	expectedFindFilter.PerPage = &perPageAll

	db.Scene.On("Query", mock.Anything, models.SceneQueryOptions{
		QueryOptions: models.QueryOptions{
			FindFilter: &expectedFindFilter,
			Count:      false,
		},
		SceneFilter: sceneFilter,
	}).Return(&models.SceneQueryResult{
		QueryResult: models.QueryResult[int]{
			IDs: []int{10, 20, 30},
		},
	}, nil).Once()

	input := BulkSceneUpdateInput{
		Ids:                         []string{}, // Empty IDs, rely on filter
		ApplyToItemsMatchingFilters: PtrBool(true),
		SceneFilter:                 sceneFilter,
		FindFilter:                  findFilter,
		Rating100:                   PtrInt(80),
	}

	// Wrap in a context that has the input map and gql context
	inputMap := map[string]interface{}{
		"input": map[string]interface{}{
			"apply_to_items_matching_filters": true,
			"rating100":                       80,
		},
	}
	ctx := withGqlContext(testCtx, inputMap)

	jobID, err := r.Mutation().BulkSceneUpdateJob(ctx, input)

	assert.Nil(t, err)
	assert.Equal(t, "1", jobID)
	assert.Len(t, bulkUpdater.calls, 1)
	assert.Equal(t, "Bulk Scene Update", bulkUpdater.calls[0].description)
	assert.Equal(t, []int{10, 20, 30}, bulkUpdater.calls[0].ids)

	db.AssertExpectations(t)
}

func TestBulkSceneUpdate_SelectedIDsIgnoresFiltersWithoutApplyToAll(t *testing.T) {
	db := mocks.NewDatabase()
	r, bulkUpdater := newResolver(db)

	input := BulkSceneUpdateInput{
		Ids: []string{"10", "20"},
		FindFilter: &models.FindFilterType{
			Sort: PtrString("title"),
		},
		SceneFilter: &models.SceneFilterType{
			Rating100: &models.IntCriterionInput{
				Value:    50,
				Modifier: models.CriterionModifierGreaterThan,
			},
		},
		Rating100: PtrInt(80),
	}

	inputMap := map[string]interface{}{
		"input": map[string]interface{}{
			"rating100": 80,
		},
	}
	ctx := withGqlContext(testCtx, inputMap)

	expectedPartial := mock.MatchedBy(func(partial models.ScenePartial) bool {
		return partial.UpdatedAt.Set &&
			partial.Rating.Set &&
			!partial.Rating.Null &&
			partial.Rating.Value == 80
	})

	db.Scene.On("UpdatePartial", mock.Anything, 10, expectedPartial).
		Return(&models.Scene{ID: 10}, nil).Once()
	db.Scene.On("UpdatePartial", mock.Anything, 20, expectedPartial).
		Return(&models.Scene{ID: 20}, nil).Once()

	result, err := r.Mutation().BulkSceneUpdateJob(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "sync", result)
	assert.Empty(t, bulkUpdater.calls)
	db.Scene.AssertNotCalled(t, "Query", mock.Anything, mock.Anything)
	db.AssertExpectations(t)
}

func TestBulkSceneUpdate_SelectedIDsRunsSynchronously(t *testing.T) {
	db := mocks.NewDatabase()
	r, bulkUpdater := newResolver(db)

	input := BulkSceneUpdateInput{
		Ids:       []string{"10", "20"},
		Rating100: PtrInt(80),
	}

	inputMap := map[string]interface{}{
		"input": map[string]interface{}{
			"rating100": 80,
		},
	}
	ctx := withGqlContext(testCtx, inputMap)

	expectedPartial := mock.MatchedBy(func(partial models.ScenePartial) bool {
		return partial.UpdatedAt.Set &&
			partial.Rating.Set &&
			!partial.Rating.Null &&
			partial.Rating.Value == 80
	})

	db.Scene.On("UpdatePartial", mock.Anything, 10, expectedPartial).
		Return(&models.Scene{ID: 10}, nil).Once()
	db.Scene.On("UpdatePartial", mock.Anything, 20, expectedPartial).
		Return(&models.Scene{ID: 20}, nil).Once()

	result, err := r.Mutation().BulkSceneUpdateJob(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, "sync", result)
	assert.Empty(t, bulkUpdater.calls)
	db.AssertExpectations(t)
}

func TestBulkSceneUpdate_SelectedIDsGraphQLInputMap(t *testing.T) {
	inputMap := map[string]interface{}{
		"input": map[string]interface{}{
			"rating100": 80,
		},
	}
	ctx := withGqlContext(testCtx, inputMap)

	got := getUpdateInputMap(ctx)

	assert.Equal(t, 80, got["rating100"])
}
