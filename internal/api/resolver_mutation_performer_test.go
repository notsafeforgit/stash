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

	destination := &models.Performer{ID: destinationID, Name: "Destination Name"}
	source := &models.Performer{ID: sourceID, Name: sourceName}
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

func TestPerformerMergeKeepsDestinationUpdateBeforeMerge(t *testing.T) {
	db := mocks.NewDatabase()
	r, _ := newResolver(db)

	destination := &models.Performer{ID: 2, Name: "Destination Name"}
	source := &models.Performer{ID: 1, Name: "Source Name"}

	findDestination := db.Performer.On("Find", mock.Anything, 2).
		Return(destination, nil).Once()
	findSources := db.Performer.On("FindMany", mock.Anything, []int{1}).
		Return([]*models.Performer{source}, nil).Once()
	applyValues := db.Performer.On(
		"UpdatePartial",
		mock.Anything,
		2,
		mock.MatchedBy(func(partial models.PerformerPartial) bool {
			return !partial.Name.Set && !partial.Disambiguation.Set
		}),
	).Return(destination, nil).Once()
	mergeSources := db.Performer.On("Merge", mock.Anything, []int{1}, 2).
		Return(nil).Once()
	mock.InOrder(findDestination, findSources, applyValues, mergeSources)

	ctx := withGqlContext(testCtx, map[string]interface{}{
		"input": map[string]interface{}{
			"values": map[string]interface{}{},
		},
	})
	result, err := r.Mutation().PerformerMerge(ctx, PerformerMergeInput{
		Source:      []string{"1"},
		Destination: "2",
		Values:      &models.PerformerUpdateInput{ID: "2"},
	})

	assert.NoError(t, err)
	assert.Equal(t, 2, result.ID)
	db.AssertExpectations(t)
}
