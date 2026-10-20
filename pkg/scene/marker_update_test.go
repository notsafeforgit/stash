package scene

import (
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAdjustMarkerTagsPreservesSameLengthReplacement(t *testing.T) {
	repo := &mocks.SceneMarkerReaderWriter{}
	repo.On("Find", mock.Anything, 1).Return(&models.SceneMarker{ID: 1, PrimaryTagID: 2}, nil)
	repo.On("GetTagIDs", mock.Anything, 1).Return([]int{3}, nil)
	partial := models.SceneMarkerPartial{TagIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{2, 4}}}
	require.NoError(t, AdjustMarkerTags(t.Context(), repo, 1, &partial))
	require.Equal(t, &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{4}}, partial.TagIDs)
	repo.AssertExpectations(t)
}

func TestAdjustMarkerTagsMissingMarkerReturnsError(t *testing.T) {
	repo := &mocks.SceneMarkerReaderWriter{}
	repo.On("Find", mock.Anything, 1).Return((*models.SceneMarker)(nil), nil)
	partial := models.SceneMarkerPartial{TagIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeSet, IDs: []int{4}}}
	require.ErrorContains(t, AdjustMarkerTags(t.Context(), repo, 1, &partial), "not found")
	repo.AssertExpectations(t)
}
