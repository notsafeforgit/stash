package api

import (
	"context"
	"math"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestEntityImageFromSceneCover(t *testing.T) {
	db := mocks.NewDatabase()
	resolver, _ := newResolver(db)
	r := &mutationResolver{resolver}
	cover := []byte("existing scene cover")
	db.Scene.On("Find", mock.Anything, 3).Return(&models.Scene{ID: 3}, nil).Once()
	db.Scene.On("GetCover", mock.Anything, 3).Return(cover, nil).Once()

	data, err := r.entityImageDataFromScene(context.Background(), models.SceneImageInput{ID: "3"})
	require.NoError(t, err)
	require.Equal(t, cover, data)
	// No scene updates or file access are expected when copying a stored cover.
	db.AssertExpectations(t)
}

func TestEntityImageFromSceneMissingSource(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing scene", true: "missing cover"}[exists], func(t *testing.T) {
			db := mocks.NewDatabase()
			resolver, _ := newResolver(db)
			r := &mutationResolver{resolver}
			var scene *models.Scene
			if exists {
				scene = &models.Scene{ID: 3}
				db.Scene.On("GetCover", mock.Anything, 3).Return([]byte(nil), nil).Once()
			}
			db.Scene.On("Find", mock.Anything, 3).Return(scene, nil).Once()
			data, err := r.entityImageDataFromScene(context.Background(), models.SceneImageInput{ID: "3"})
			require.Error(t, err)
			require.Empty(t, data)
			db.AssertExpectations(t)
		})
	}
}

func TestEntityImageFromSceneRejectsInvalidTime(t *testing.T) {
	for _, at := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		r := &mutationResolver{}
		_, err := r.entityImageDataFromScene(context.Background(), models.SceneImageInput{ID: "3", At: &at})
		require.ErrorContains(t, err, "finite, non-negative")
	}
}

func TestEntityImageRequiresSingleSource(t *testing.T) {
	data := ""
	id := "1"
	scene := &models.SceneImageInput{ID: id}
	for _, input := range []models.EntityImageInput{
		{},
		{Data: &data, ImageID: &id},
		{Data: &data, Scene: scene},
		{ImageID: &id, Scene: scene},
		{Data: &data, ImageID: &id, Scene: scene},
	} {
		r := &mutationResolver{}
		_, _, err := r.processEntityImageInputObject(context.Background(), &input)
		require.ErrorContains(t, err, "exactly one")
	}
}
