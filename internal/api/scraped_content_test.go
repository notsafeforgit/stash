package api

import (
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

// Cache.ScrapeURL returns nil content when no scraper supports a URL or a
// scraper finds nothing. URL scrape resolvers must return a null result for
// that case instead of panicking (stashapp/stash#7333).
func TestMarshalSingleScrapedContentNil(t *testing.T) {
	performer, err := marshalScrapedPerformer(nil)
	require.NoError(t, err)
	require.Nil(t, performer)

	scene, err := marshalScrapedScene(nil)
	require.NoError(t, err)
	require.Nil(t, scene)

	gallery, err := marshalScrapedGallery(nil)
	require.NoError(t, err)
	require.Nil(t, gallery)

	image, err := marshalScrapedImage(nil)
	require.NoError(t, err)
	require.Nil(t, image)

	group, err := marshalScrapedGroup(nil)
	require.NoError(t, err)
	require.Nil(t, group)
}

func TestMarshalSingleScrapedContent(t *testing.T) {
	name := "Performer"
	performer, err := marshalScrapedPerformer(&models.ScrapedPerformer{Name: &name})
	require.NoError(t, err)
	require.NotNil(t, performer)
	require.Equal(t, name, *performer.Name)

	title := "Scene"
	scene, err := marshalScrapedScene(models.ScrapedScene{Title: &title})
	require.NoError(t, err)
	require.NotNil(t, scene)
	require.Equal(t, title, *scene.Title)

	_, err = marshalScrapedScene(&models.ScrapedPerformer{Name: &name})
	require.True(t, errors.Is(err, models.ErrConversion), err)
}
