package scraper

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type nativeScrapedGroupFinder struct {
	names []string
}

func (f *nativeScrapedGroupFinder) FindByNames(_ context.Context, names []string, _ bool) ([]*models.Group, error) {
	f.names = append(f.names, names...)
	return []*models.Group{{ID: 12, Name: names[0]}}, nil
}

func TestNativeScrapedGroupNormalization(t *testing.T) {
	ctx := context.Background()
	name, firstURL := "Provider group", "https://example.test/group"
	legacy := models.ScrapedMovie{Name: &name, URL: &firstURL}
	for _, input := range []ScrapedContent{legacy, &legacy} {
		pp := &postScraper{}
		result, err := pp.postScrape(ctx, input)
		require.NoError(t, err)
		g, ok := result.(models.ScrapedGroup)
		require.True(t, ok, "provider movie input must become native group output")
		require.Equal(t, &name, g.Name)
		require.Equal(t, []string{firstURL}, g.URLs)
	}
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy scene input", true: "explicit native precedence"}[native], func(t *testing.T) {
			finder := &nativeScrapedGroupFinder{}
			pp := &postScraper{Cache: Cache{repository: Repository{GroupFinder: finder}}}
			input := models.ScrapedScene{Movies: []*models.ScrapedMovie{nil, &legacy}}
			wantName, wantURLs := name, []string{firstURL}
			if native {
				wantName = "Native choice"
				wantURLs = []string{firstURL, "https://example.test/other"}
				input.Groups = []*models.ScrapedGroup{{Name: &wantName, URLs: wantURLs}}
			}
			result, err := pp.postScrape(ctx, input)
			require.NoError(t, err)
			scene := result.(models.ScrapedScene)
			require.Nil(t, scene.Movies, "never project a second relationship representation")
			require.Len(t, scene.Groups, 1)
			require.Equal(t, &wantName, scene.Groups[0].Name)
			require.Equal(t, wantURLs, scene.Groups[0].URLs)
			require.Equal(t, "12", *scene.Groups[0].StoredID)
			require.Equal(t, []string{wantName}, finder.names, "match the chosen relationship only once")
		})
	}
}
