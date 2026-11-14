package api

import (
	"fmt"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scraper"
)

// marshalScrapedScenes converts ScrapedContent into ScrapedScene. If conversion fails, an
// error is returned to the caller.
func marshalScrapedScenes(content []scraper.ScrapedContent) ([]*models.ScrapedScene, error) {
	var ret []*models.ScrapedScene
	for _, c := range content {
		if c == nil {
			// graphql schema requires scenes to be non-nil
			continue
		}

		switch s := c.(type) {
		case *models.ScrapedScene:
			ret = append(ret, s)
		case models.ScrapedScene:
			ret = append(ret, &s)
		default:
			return nil, fmt.Errorf("%w: cannot turn ScrapedContent into ScrapedScene", models.ErrConversion)
		}
	}

	return ret, nil
}

// marshalScrapedPerformers converts ScrapedContent into ScrapedPerformer. If conversion
// fails, an error is returned to the caller.
func marshalScrapedPerformers(content []scraper.ScrapedContent) ([]*models.ScrapedPerformer, error) {
	var ret []*models.ScrapedPerformer
	for _, c := range content {
		if c == nil {
			// graphql schema requires performers to be non-nil
			continue
		}

		switch p := c.(type) {
		case *models.ScrapedPerformer:
			ret = append(ret, p)
		case models.ScrapedPerformer:
			ret = append(ret, &p)
		default:
			return nil, fmt.Errorf("%w: cannot turn ScrapedContent into ScrapedPerformer", models.ErrConversion)
		}
	}

	return ret, nil
}

// marshalScrapedGalleries converts ScrapedContent into ScrapedGallery. If
// conversion fails, an error is returned.
func marshalScrapedGalleries(content []scraper.ScrapedContent) ([]*models.ScrapedGallery, error) {
	var ret []*models.ScrapedGallery
	for _, c := range content {
		if c == nil {
			// graphql schema requires galleries to be non-nil
			continue
		}

		switch g := c.(type) {
		case *models.ScrapedGallery:
			ret = append(ret, g)
		case models.ScrapedGallery:
			ret = append(ret, &g)
		default:
			return nil, fmt.Errorf("%w: cannot turn ScrapedContent into ScrapedGallery", models.ErrConversion)
		}
	}

	return ret, nil
}

func marshalScrapedImages(content []scraper.ScrapedContent) ([]*models.ScrapedImage, error) {
	var ret []*models.ScrapedImage
	for _, c := range content {
		if c == nil {
			// graphql schema requires images to be non-nil
			continue
		}

		switch g := c.(type) {
		case *models.ScrapedImage:
			ret = append(ret, g)
		case models.ScrapedImage:
			ret = append(ret, &g)
		default:
			return nil, fmt.Errorf("%w: cannot turn ScrapedContent into ScrapedImage", models.ErrConversion)
		}
	}

	return ret, nil
}

// marshalScrapedGroups converts provider content into native groups. If conversion
// fails, an error is returned.
func marshalScrapedGroups(content []scraper.ScrapedContent) ([]*models.ScrapedGroup, error) {
	var ret []*models.ScrapedGroup
	for _, c := range content {
		if c == nil {
			// graphql schema requires groups to be non-nil
			continue
		}

		switch m := c.(type) {
		case *models.ScrapedGroup:
			ret = append(ret, m)
		case models.ScrapedGroup:
			ret = append(ret, &m)
		// it's possible that a scraper returns models.ScrapedMovie
		case *models.ScrapedMovie:
			g := m.ScrapedGroup()
			ret = append(ret, &g)
		case models.ScrapedMovie:
			g := m.ScrapedGroup()
			ret = append(ret, &g)
		default:
			return nil, fmt.Errorf("%w: cannot turn ScrapedContent into ScrapedGroup", models.ErrConversion)
		}
	}

	return ret, nil
}

// marshalScrapedPerformer will marshal a single performer
func marshalScrapedPerformer(content scraper.ScrapedContent) (*models.ScrapedPerformer, error) {
	return firstScraped(marshalScrapedPerformers([]scraper.ScrapedContent{content}))
}

// marshalScrapedScene will marshal a single scraped scene
func marshalScrapedScene(content scraper.ScrapedContent) (*models.ScrapedScene, error) {
	return firstScraped(marshalScrapedScenes([]scraper.ScrapedContent{content}))
}

// marshalScrapedGallery will marshal a single scraped gallery
func marshalScrapedGallery(content scraper.ScrapedContent) (*models.ScrapedGallery, error) {
	return firstScraped(marshalScrapedGalleries([]scraper.ScrapedContent{content}))
}

// marshalScrapedImage will marshal a single scraped image
func marshalScrapedImage(content scraper.ScrapedContent) (*models.ScrapedImage, error) {
	return firstScraped(marshalScrapedImages([]scraper.ScrapedContent{content}))
}

// marshalScrapedGroup will marshal a single scraped group
func marshalScrapedGroup(content scraper.ScrapedContent) (*models.ScrapedGroup, error) {
	return firstScraped(marshalScrapedGroups([]scraper.ScrapedContent{content}))
}

// firstScraped returns the single marshalled item. A scraper returns nil
// content when no scraper supports a URL or nothing was found; the plural
// marshallers skip nil content, so that case yields a nil result rather than
// indexing an empty slice.
func firstScraped[T any](items []*T, err error) (*T, error) {
	if err != nil || len(items) == 0 {
		return nil, err
	}

	return items[0], nil
}
