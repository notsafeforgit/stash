package api

import (
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestPerformerMergeRemovesSourcesBeforeApplyingValues(t *testing.T) {
	db := mocks.NewDatabase()
	r, _ := newResolver(db)

	const (
		sourceID      = 1
		destinationID = 2
		sourceName    = "Source Name"
	)

	destination := &models.Performer{ID: destinationID, Name: "Destination Name"}
	source := &models.Performer{ID: sourceID, Name: sourceName}
	updated := &models.Performer{ID: destinationID, Name: sourceName}

	findDestination := db.Performer.On("Find", mock.Anything, destinationID).
		Return(destination, nil).Once()
	findSources := db.Performer.On("FindMany", mock.Anything, []int{sourceID}).
		Return([]*models.Performer{source}, nil).Once()
	mergeSources := db.Performer.On("Merge", mock.Anything, []int{sourceID}, destinationID).
		Return(nil).Once()
	applyValues := db.Performer.On(
		"UpdatePartial",
		mock.Anything,
		destinationID,
		mock.MatchedBy(func(partial models.PerformerPartial) bool {
			return partial.Name.Set && partial.Name.Value == sourceName
		}),
	).Return(updated, nil).Once()
	mock.InOrder(findDestination, findSources, mergeSources, applyValues)

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
