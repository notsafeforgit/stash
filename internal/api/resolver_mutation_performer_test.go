package api

import (
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestPerformerImageSourceFromURL(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		wantID int
		wantOK bool
	}{
		{name: "absolute", url: "https://stash.example/base/performer/12/image?t=1", wantID: 12, wantOK: true},
		{name: "relative", url: "/performer/12/image?t=1", wantID: 12, wantOK: true},
		{name: "unrelated performer", url: "/performer/13/image?t=1"},
		{name: "external image", url: "https://example.com/image.jpg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := performerImageSourceFromURL(tt.url, []int{12})
			assert.Equal(t, tt.wantID, gotID)
			assert.Equal(t, tt.wantOK, gotOK)
		})
	}
}

func TestPerformerMergeMovesSourceNameBeforeApplyingValues(t *testing.T) {
	db := mocks.NewDatabase()
	r, _ := newResolver(db)

	const (
		sourceID      = 1
		destinationID = 2
		sourceName    = "Source Name"
	)

	destination := &models.Performer{
		ID:      destinationID,
		Name:    "Destination Name",
		Aliases: models.NewRelatedPerformerAliases([]models.PerformerAlias{}),
	}
	source := &models.Performer{
		ID:      sourceID,
		Name:    sourceName,
		Aliases: models.NewRelatedPerformerAliases([]models.PerformerAlias{}),
	}
	updated := &models.Performer{ID: destinationID, Name: sourceName}

	findDestination := db.Performer.On("Find", mock.Anything, destinationID).
		Return(destination, nil).Once()
	findSources := db.Performer.On("FindMany", mock.Anything, []int{sourceID}).
		Return([]*models.Performer{source}, nil).Once()
	prepareSource := db.Performer.On(
		"UpdatePartial",
		mock.Anything,
		sourceID,
		mock.MatchedBy(func(partial models.PerformerPartial) bool {
			return partial.Name.Set && partial.Name.Value != sourceName
		}),
	).Return(source, nil).Once()
	applyValues := db.Performer.On(
		"UpdatePartial",
		mock.Anything,
		destinationID,
		mock.MatchedBy(func(partial models.PerformerPartial) bool {
			return partial.Name.Set && partial.Name.Value == sourceName
		}),
	).Return(updated, nil).Once()
	mergeSources := db.Performer.On("Merge", mock.Anything, []int{sourceID}, destinationID).
		Return(nil).Once()
	mock.InOrder(findDestination, findSources, prepareSource, applyValues, mergeSources)

	ctx := withGqlContext(testCtx, map[string]interface{}{
		"input": map[string]interface{}{
			"values": map[string]interface{}{"name": sourceName},
		},
	})
	result, err := r.Mutation().PerformerMerge(ctx, PerformerMergeInput{
		Source:      []string{"1"},
		Destination: "2",
		Values: &models.PerformerUpdateInput{
			ID:   "2",
			Name: PtrString(sourceName),
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, destinationID, result.ID)
	db.AssertExpectations(t)
}

func TestPerformerMergePreservesSourceNameWithoutValues(t *testing.T) {
	db := mocks.NewDatabase()
	r, _ := newResolver(db)

	destination := &models.Performer{
		ID:      2,
		Name:    "Destination Name",
		Aliases: models.NewRelatedPerformerAliases([]models.PerformerAlias{}),
	}
	source := &models.Performer{
		ID:      1,
		Name:    "Source Name",
		Aliases: models.NewRelatedPerformerAliases([]models.PerformerAlias{}),
	}

	findDestination := db.Performer.On("Find", mock.Anything, 2).
		Return(destination, nil).Once()
	findSources := db.Performer.On("FindMany", mock.Anything, []int{1}).
		Return([]*models.Performer{source}, nil).Once()
	applyValues := db.Performer.On(
		"UpdatePartial",
		mock.Anything,
		2,
		mock.MatchedBy(func(partial models.PerformerPartial) bool {
			return !partial.Name.Set &&
				!partial.Disambiguation.Set &&
				partial.Aliases != nil &&
				partial.Aliases.Mode == models.RelationshipUpdateModeSet &&
				assert.Equal(t, []models.PerformerAlias{{Alias: "Source Name"}}, partial.Aliases.Values)
		}),
	).Return(destination, nil).Once()
	mergeSources := db.Performer.On("Merge", mock.Anything, []int{1}, 2).
		Return(nil).Once()
	mock.InOrder(findDestination, findSources, applyValues, mergeSources)

	ctx := withGqlContext(testCtx, map[string]interface{}{
		"input": map[string]interface{}{},
	})
	result, err := r.Mutation().PerformerMerge(ctx, PerformerMergeInput{
		Source:      []string{"1"},
		Destination: "2",
	})

	assert.NoError(t, err)
	assert.Equal(t, 2, result.ID)
	db.AssertExpectations(t)
}

func TestPerformerMergePreservesPoliciesFromLegacyAliasList(t *testing.T) {
	db := mocks.NewDatabase()
	r, _ := newResolver(db)

	destination := &models.Performer{
		ID:   2,
		Name: "Destination Name",
		Aliases: models.NewRelatedPerformerAliases([]models.PerformerAlias{
			{Alias: "Existing Alias"},
		}),
	}
	source := &models.Performer{
		ID:      1,
		Name:    "Source Name",
		Aliases: models.NewRelatedPerformerAliases([]models.PerformerAlias{}),
	}

	findDestination := db.Performer.On("Find", mock.Anything, 2).
		Return(destination, nil).Once()
	findSources := db.Performer.On("FindMany", mock.Anything, []int{1}).
		Return([]*models.Performer{source}, nil).Once()
	applyValues := db.Performer.On(
		"UpdatePartial",
		mock.Anything,
		2,
		mock.MatchedBy(func(partial models.PerformerPartial) bool {
			return partial.Aliases != nil && assert.Equal(t, []models.PerformerAlias{
				{Alias: "Existing Alias"},
				{Alias: "Source Name"},
			}, partial.Aliases.Values)
		}),
	).Return(destination, nil).Once()
	mergeSources := db.Performer.On("Merge", mock.Anything, []int{1}, 2).
		Return(nil).Once()
	mock.InOrder(findDestination, findSources, applyValues, mergeSources)

	ctx := withGqlContext(testCtx, map[string]interface{}{
		"input": map[string]interface{}{
			"values": map[string]interface{}{
				"alias_list": []interface{}{"Existing Alias", "Source Name"},
			},
		},
	})
	result, err := r.Mutation().PerformerMerge(ctx, PerformerMergeInput{
		Source:      []string{"1"},
		Destination: "2",
		Values: &models.PerformerUpdateInput{
			ID:        "2",
			AliasList: []string{"Existing Alias", "Source Name"},
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, 2, result.ID)
	db.AssertExpectations(t)
}

func TestPerformerMergeSupportsNormalizedImageInput(t *testing.T) {
	db := mocks.NewDatabase()
	r, _ := newResolver(db)

	destination := &models.Performer{
		ID:      2,
		Name:    "Destination",
		Aliases: models.NewRelatedPerformerAliases([]models.PerformerAlias{}),
	}
	source := &models.Performer{
		ID:      1,
		Name:    "Source",
		Aliases: models.NewRelatedPerformerAliases([]models.PerformerAlias{}),
	}
	clearedImage := ""

	findDestination := db.Performer.On("Find", mock.Anything, 2).
		Return(destination, nil).Once()
	findSources := db.Performer.On("FindMany", mock.Anything, []int{1}).
		Return([]*models.Performer{source}, nil).Once()
	applyValues := db.Performer.On("UpdatePartial", mock.Anything, 2, mock.Anything).
		Return(destination, nil).Once()
	mergeSources := db.Performer.On("Merge", mock.Anything, []int{1}, 2).
		Return(nil).Once()
	clearDestinationImage := db.Performer.On("UpdateImage", mock.Anything, 2, []byte(nil)).
		Return(nil).Once()
	mock.InOrder(findDestination, findSources, applyValues, mergeSources, clearDestinationImage)

	ctx := withGqlContext(testCtx, map[string]interface{}{
		"input": map[string]interface{}{
			"values": map[string]interface{}{
				"image_input": map[string]interface{}{"data": ""},
			},
		},
	})
	result, err := r.Mutation().PerformerMerge(ctx, PerformerMergeInput{
		Source:      []string{"1"},
		Destination: "2",
		Values: &models.PerformerUpdateInput{
			ID: "2",
			ImageInput: &models.EntityImageInput{
				Data: &clearedImage,
			},
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, destination, result)
	db.AssertExpectations(t)
}

func TestPerformerMergeReturnsErrorForMissingDestination(t *testing.T) {
	db := mocks.NewDatabase()
	r, _ := newResolver(db)
	db.Performer.On("Find", mock.Anything, 2).Return(nil, nil).Once()

	result, err := r.Mutation().PerformerMerge(testCtx, PerformerMergeInput{
		Source:      []string{"1"},
		Destination: "2",
	})

	assert.Nil(t, result)
	assert.EqualError(t, err, "destination performer ID 2 not found")
	db.AssertExpectations(t)
}

func TestPerformerMergeSafeModeRejectsUnresolvedSourceData(t *testing.T) {
	db := mocks.NewDatabase()
	r, _ := newResolver(db)

	destination := &models.Performer{
		ID:       2,
		Name:     "Destination",
		Aliases:  models.NewRelatedPerformerAliases([]models.PerformerAlias{}),
		URLs:     models.NewRelatedStrings([]string{}),
		StashIDs: models.NewRelatedStashIDs([]models.StashID{}),
	}
	source := &models.Performer{
		ID:       1,
		Name:     "Source",
		Details:  "source-only details",
		Aliases:  models.NewRelatedPerformerAliases([]models.PerformerAlias{}),
		URLs:     models.NewRelatedStrings([]string{}),
		StashIDs: models.NewRelatedStashIDs([]models.StashID{}),
	}
	db.Performer.On("Find", mock.Anything, 2).Return(destination, nil).Once()
	db.Performer.On("FindMany", mock.Anything, []int{1}).
		Return([]*models.Performer{source}, nil).Once()
	db.Performer.On("GetCustomFields", mock.Anything, 2).
		Return(map[string]interface{}{}, nil).Once()
	db.Performer.On("GetCustomFields", mock.Anything, 1).
		Return(map[string]interface{}{}, nil).Once()
	db.Performer.On("HasImage", mock.Anything, 1).Return(false, nil).Once()
	requireResolved := true

	result, err := r.Mutation().PerformerMerge(testCtx, PerformerMergeInput{
		Source:                []string{"1"},
		Destination:           "2",
		RequireResolvedValues: &requireResolved,
	})

	assert.Nil(t, result)
	assert.EqualError(t, err, "performer merge would discard unresolved source values for fields: details; provide each field in input.values")
	db.AssertExpectations(t)
}

func TestPerformerMergeSafeModeAppliesExplicitResolution(t *testing.T) {
	db := mocks.NewDatabase()
	r, _ := newResolver(db)

	destination := &models.Performer{
		ID:       2,
		Name:     "Destination",
		Aliases:  models.NewRelatedPerformerAliases([]models.PerformerAlias{}),
		URLs:     models.NewRelatedStrings([]string{}),
		StashIDs: models.NewRelatedStashIDs([]models.StashID{}),
	}
	source := &models.Performer{
		ID:       1,
		Name:     "Source",
		Details:  "source-only details",
		Aliases:  models.NewRelatedPerformerAliases([]models.PerformerAlias{}),
		URLs:     models.NewRelatedStrings([]string{}),
		StashIDs: models.NewRelatedStashIDs([]models.StashID{}),
	}
	updated := &models.Performer{ID: 2, Name: "Destination", Details: "Destination choice"}
	updatedAfterTags := &models.Performer{ID: 2, Name: "Destination", Details: "Destination choice"}

	findDestination := db.Performer.On("Find", mock.Anything, 2).
		Return(destination, nil).Once()
	findSources := db.Performer.On("FindMany", mock.Anything, []int{1}).
		Return([]*models.Performer{source}, nil).Once()
	loadDestinationCustomFields := db.Performer.On("GetCustomFields", mock.Anything, 2).
		Return(map[string]interface{}{}, nil).Once()
	loadSourceCustomFields := db.Performer.On("GetCustomFields", mock.Anything, 1).
		Return(map[string]interface{}{}, nil).Once()
	checkSourceImage := db.Performer.On("HasImage", mock.Anything, 1).
		Return(false, nil).Once()
	validateDestination := db.Performer.On("Find", mock.Anything, 2).
		Return(destination, nil).Once()
	applyValues := db.Performer.On(
		"UpdatePartial",
		mock.Anything,
		2,
		mock.MatchedBy(func(partial models.PerformerPartial) bool {
			return partial.Details.Set &&
				partial.Details.Value == "Destination choice" &&
				partial.TagIDs == nil &&
				partial.Aliases != nil &&
				assert.Equal(t, []models.PerformerAlias{{Alias: "Source"}}, partial.Aliases.Values)
		}),
	).Return(updated, nil).Once()
	mergeSources := db.Performer.On("Merge", mock.Anything, []int{1}, 2).
		Return(nil).Once()
	applyResolvedTags := db.Performer.On(
		"UpdatePartial",
		mock.Anything,
		2,
		mock.MatchedBy(func(partial models.PerformerPartial) bool {
			return partial.TagIDs != nil &&
				partial.TagIDs.Mode == models.RelationshipUpdateModeSet &&
				assert.Equal(t, []int{10}, partial.TagIDs.IDs)
		}),
	).Return(updatedAfterTags, nil).Once()
	mock.InOrder(
		findDestination,
		findSources,
		loadDestinationCustomFields,
		loadSourceCustomFields,
		checkSourceImage,
		validateDestination,
		applyValues,
		mergeSources,
		applyResolvedTags,
	)

	requireResolved := true
	ctx := withGqlContext(testCtx, map[string]interface{}{
		"input": map[string]interface{}{
			"values": map[string]interface{}{
				"details": "Destination choice",
				"tag_ids": []interface{}{"10"},
			},
		},
	})
	result, err := r.Mutation().PerformerMerge(ctx, PerformerMergeInput{
		Source:                []string{"1"},
		Destination:           "2",
		RequireResolvedValues: &requireResolved,
		Values: &models.PerformerUpdateInput{
			ID:      "2",
			Details: PtrString("Destination choice"),
			TagIds:  []string{"10"},
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, updatedAfterTags, result)
	db.AssertExpectations(t)
}

func TestPerformerMergeSafeModeValidatesInputShape(t *testing.T) {
	db := mocks.NewDatabase()
	r, _ := newResolver(db)
	requireResolved := true

	result, err := r.Mutation().PerformerMerge(testCtx, PerformerMergeInput{
		Destination:           "2",
		RequireResolvedValues: &requireResolved,
	})
	assert.Nil(t, result)
	assert.EqualError(t, err, "safe performer merge requires at least one source performer")

	result, err = r.Mutation().PerformerMerge(testCtx, PerformerMergeInput{
		Source:                []string{"1"},
		Destination:           "2",
		RequireResolvedValues: &requireResolved,
		Values:                &models.PerformerUpdateInput{ID: "3"},
	})
	assert.Nil(t, result)
	assert.EqualError(t, err, "performer merge values id must match destination id")
	db.AssertExpectations(t)
}
