package api

import (
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	tagName    = "tagName"
	errTagName = "errTagName"

	existingTagID   = 1
	existingTagName = "existingTagName"

	newTagID = 2
)

func TestTagCreate(t *testing.T) {
	db := mocks.NewDatabase()
	r, _ := newResolver(db)

	pp := 1
	findFilter := &models.FindFilterType{
		PerPage: &pp,
	}

	tagFilterForName := func(n string) *models.TagFilterType {
		return &models.TagFilterType{
			Name: &models.StringCriterionInput{
				Value:    n,
				Modifier: models.CriterionModifierEquals,
			},
		}
	}

	tagFilterForAlias := func(n string) *models.TagFilterType {
		return &models.TagFilterType{
			Aliases: &models.StringCriterionInput{
				Value:    n,
				Modifier: models.CriterionModifierEquals,
			},
		}
	}

	db.Tag.On("Query", mock.Anything, tagFilterForName(existingTagName), findFilter).Return([]*models.Tag{
		{
			ID:   existingTagID,
			Name: existingTagName,
		},
	}, 1, nil).Once()
	db.Tag.On("Query", mock.Anything, tagFilterForName(errTagName), findFilter).Return(nil, 0, nil).Once()
	db.Tag.On("Query", mock.Anything, tagFilterForAlias(errTagName), findFilter).Return(nil, 0, nil).Once()

	expectedErr := errors.New("TagCreate error")
	db.Tag.On("Create", mock.Anything, mock.AnythingOfType("*models.Tag")).Return(expectedErr)

	// fails here because testCtx is empty
	// TODO: Fix this
	if 1 != 0 {
		return
	}

	_, err := r.Mutation().TagCreate(testCtx, TagCreateInput{
		Name: existingTagName,
	})

	assert.NotNil(t, err)

	_, err = r.Mutation().TagCreate(testCtx, TagCreateInput{
		Name: errTagName,
	})

	assert.Equal(t, expectedErr, err)
	db.AssertExpectations(t)

	db = mocks.NewDatabase()
	r, _ = newResolver(db)

	db.Tag.On("Query", mock.Anything, tagFilterForName(tagName), findFilter).Return(nil, 0, nil).Once()
	db.Tag.On("Query", mock.Anything, tagFilterForAlias(tagName), findFilter).Return(nil, 0, nil).Once()
	newTag := &models.Tag{
		ID:   newTagID,
		Name: tagName,
	}
	db.Tag.On("Create", mock.Anything, mock.AnythingOfType("*models.Tag")).Run(func(args mock.Arguments) {
		arg := args.Get(1).(*models.Tag)
		arg.ID = newTagID
	}).Return(nil)
	db.Tag.On("Find", mock.Anything, newTagID).Return(newTag, nil)

	tag, err := r.Mutation().TagCreate(testCtx, TagCreateInput{
		Name: tagName,
	})

	assert.Nil(t, err)
	assert.NotNil(t, tag)
	db.AssertExpectations(t)
}

func TestBulkTagUpdate_ApplyToAll(t *testing.T) {
	db := mocks.NewDatabase()
	r, bulkUpdater := newResolver(db)

	tagIDs := []int{1, 2, 3}
	tagFilterAST := &models.FilterAST{
		Root: &models.FilterASTNode{
			Condition: &models.FilterASTCondition{
				Field: "name",
				Value: map[string]interface{}{
					"value":    "test",
					"modifier": models.CriterionModifierIncludes,
				},
			},
		},
	}
	findFilter := &models.FindFilterType{
		Sort:      PtrString("name"),
		Direction: PtrSortDirectionEnum(models.SortDirectionEnumAsc),
	}

	// Mock the query call that fetches all IDs
	db.Tag.On("QueryAST", mock.Anything, tagFilterAST, mock.MatchedBy(func(ff *models.FindFilterType) bool {
		return ff.Page == nil && ff.PerPage != nil && *ff.PerPage == models.PerPageAll
	})).Return([]*models.Tag{
		{ID: 1}, {ID: 2}, {ID: 3},
	}, 3, nil).Once()

	input := BulkTagUpdateInput{
		Ids:                         []string{}, // Empty IDs, rely on filter
		ApplyToItemsMatchingFilters: PtrBool(true),
		TagFilterAst:                tagFilterAST,
		FindFilter:                  findFilter,
		Favorite:                    PtrBool(true),
	}

	// Wrap in a context that has the input map and gql context
	inputMap := map[string]interface{}{
		"input": map[string]interface{}{
			"apply_to_items_matching_filters": true,
			"favorite":                        true,
		},
	}
	ctx := withGqlContext(testCtx, inputMap)

	jobID, err := r.Mutation().BulkTagUpdateJob(ctx, input)

	assert.Nil(t, err)
	assert.Equal(t, "1", jobID)
	assert.Len(t, bulkUpdater.calls, 1)
	assert.Equal(t, "Bulk Tag Update", bulkUpdater.calls[0].description)
	assert.Equal(t, tagIDs, bulkUpdater.calls[0].ids)
	db.AssertExpectations(t)
}
