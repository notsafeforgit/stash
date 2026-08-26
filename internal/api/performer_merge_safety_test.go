package api

import (
	"context"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func performerMergeTestPtr[T any](value T) *T {
	return &value
}

func performerForSafeMerge(id int, name string, aliases ...models.PerformerAlias) *models.Performer {
	if aliases == nil {
		aliases = []models.PerformerAlias{}
	}
	return &models.Performer{
		ID:       id,
		Name:     name,
		Aliases:  models.NewRelatedPerformerAliases(aliases),
		URLs:     models.NewRelatedStrings([]string{}),
		StashIDs: models.NewRelatedStashIDs([]models.StashID{}),
	}
}

func fullPerformerMergeSafetyFixture(t *testing.T) (*mocks.Database, *models.Performer, []*models.Performer) {
	t.Helper()

	birthdate, err := models.ParseDate("1990-01-02")
	require.NoError(t, err)
	deathDate, err := models.ParseDate("2050-03-04")
	require.NoError(t, err)
	careerStart, err := models.ParseDate("2010")
	require.NoError(t, err)
	careerEnd, err := models.ParseDate("2020")
	require.NoError(t, err)

	destination := performerForSafeMerge(2, "Destination")
	source := performerForSafeMerge(1, "Source", models.PerformerAlias{Alias: "Source Alias"})
	source.Disambiguation = "source disambiguation"
	source.Gender = performerMergeTestPtr(models.GenderEnumFemale)
	source.Birthdate = &birthdate
	source.DeathDate = &deathDate
	source.Ethnicity = "source ethnicity"
	source.Country = "source country"
	source.EyeColor = "source eyes"
	source.HairColor = "source hair"
	source.Height = performerMergeTestPtr(170)
	source.Weight = performerMergeTestPtr(60)
	source.Measurements = "source measurements"
	source.FakeTits = "source fake tits"
	source.PenisLength = performerMergeTestPtr(12.5)
	source.Circumcised = performerMergeTestPtr(models.CircumcisedEnumCut)
	source.CareerStart = &careerStart
	source.CareerEnd = &careerEnd
	source.Tattoos = "source tattoos"
	source.Piercings = "source piercings"
	source.Favorite = true
	source.Rating = performerMergeTestPtr(95)
	source.Details = "source details"
	source.IgnoreAutoTag = true
	source.URLs = models.NewRelatedStrings([]string{"https://source.example"})
	source.StashIDs = models.NewRelatedStashIDs([]models.StashID{{
		Endpoint: "https://stash.example/graphql",
		StashID:  "source-stash-id",
	}})

	db := mocks.NewDatabase()
	db.Performer.On("GetCustomFields", mock.Anything, destination.ID).
		Return(map[string]interface{}{}, nil).Once()
	db.Performer.On("GetCustomFields", mock.Anything, source.ID).
		Return(map[string]interface{}{"source field": "source value"}, nil).Once()
	db.Performer.On("HasImage", mock.Anything, source.ID).Return(true, nil).Once()

	return db, destination, []*models.Performer{source}
}

func TestPerformerMergeConflictingFieldsCoversLossyData(t *testing.T) {
	db, destination, sources := fullPerformerMergeSafetyFixture(t)

	conflicts, err := performerMergeConflictingFields(
		context.Background(),
		db.Performer,
		destination,
		sources,
	)

	require.NoError(t, err)
	assert.Len(t, conflicts, len(performerMergeSafetyFieldOrder))
	for _, field := range performerMergeSafetyFieldOrder {
		assert.Truef(t, conflicts[field], "expected conflict for %s", field)
	}
	db.AssertExpectations(t)
}

func TestValidatePerformerMergeResolvedValuesRejectsMissingFields(t *testing.T) {
	db, destination, sources := fullPerformerMergeSafetyFixture(t)

	err := validatePerformerMergeResolvedValues(
		context.Background(),
		db.Performer,
		destination,
		sources,
		changesetTranslator{},
	)

	require.EqualError(t, err, "performer merge would discard unresolved source values for fields: "+
		strings.Join(performerMergeSafetyFieldOrder, ", ")+"; provide each field in input.values")
	db.AssertExpectations(t)
}

func TestValidatePerformerMergeResolvedValuesAcceptsExplicitResolutions(t *testing.T) {
	db, destination, sources := fullPerformerMergeSafetyFixture(t)
	resolved := make(map[string]interface{}, len(performerMergeSafetyFieldOrder))
	for _, field := range performerMergeSafetyFieldOrder {
		resolved[field] = nil
	}
	delete(resolved, "aliases")
	delete(resolved, "career_start")
	delete(resolved, "career_end")
	delete(resolved, "image")
	resolved["alias_list"] = nil
	resolved["career_length"] = nil
	resolved["image_input"] = nil

	err := validatePerformerMergeResolvedValues(
		context.Background(),
		db.Performer,
		destination,
		sources,
		changesetTranslator{inputMap: resolved},
	)

	require.NoError(t, err)
	db.AssertExpectations(t)
}

func TestPerformerMergeMatchingSourceCollectionsNeedNoResolution(t *testing.T) {
	destination := performerForSafeMerge(2, "Destination", models.PerformerAlias{
		Alias: "Shared Alias",
	})
	destination.URLs = models.NewRelatedStrings([]string{"https://shared.example"})
	destination.StashIDs = models.NewRelatedStashIDs([]models.StashID{{
		Endpoint: "https://stash.example/graphql",
		StashID:  "shared-id",
	}})
	source := performerForSafeMerge(1, "Source", models.PerformerAlias{
		Alias:         "shared alias",
		IgnoreAutoTag: true,
	})
	source.URLs = models.NewRelatedStrings([]string{"https://shared.example"})
	source.StashIDs = models.NewRelatedStashIDs([]models.StashID{{
		Endpoint: "https://stash.example/graphql",
		StashID:  "shared-id",
	}})

	db := mocks.NewDatabase()
	db.Performer.On("GetCustomFields", mock.Anything, destination.ID).
		Return(map[string]interface{}{"shared": "value"}, nil).Once()
	db.Performer.On("GetCustomFields", mock.Anything, source.ID).
		Return(map[string]interface{}{"shared": "value"}, nil).Once()
	db.Performer.On("HasImage", mock.Anything, source.ID).Return(false, nil).Once()

	conflicts, err := performerMergeConflictingFields(
		context.Background(),
		db.Performer,
		destination,
		[]*models.Performer{source},
	)

	require.NoError(t, err)
	assert.Empty(t, conflicts)
	db.AssertExpectations(t)
}
