package scrape_test

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func TestSourceRunWindowAlgebraPreservesRequestedCoverage(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	random := rand.New(rand.NewPCG(13, 79))
	contains := func(windows []models.SourceWindow, at time.Time) bool {
		for _, w := range windows {
			if (w.Since == nil || !at.Before(*w.Since)) && at.Before(w.Until) {
				return true
			}
		}
		return false
	}
	for range 500 {
		groups := make([][]models.SourceWindow, 2)
		for g := range groups {
			for range random.IntN(10) {
				from := base.Add(time.Duration(random.IntN(24)) * time.Hour)
				until := from.Add(time.Duration(1+random.IntN(24)) * time.Hour)
				w := models.SourceWindow{Since: &from, Until: until}
				if random.IntN(4) == 0 {
					w.Since = nil
				}
				groups[g] = append(groups[g], w)
			}
		}
		union := scrape.Union(groups...)
		difference := scrape.Subtract(groups[0], groups[1])
		for hour := -1; hour < 50; hour++ {
			at := base.Add(time.Duration(hour) * time.Hour)
			require.Equal(t, contains(groups[0], at) || contains(groups[1], at), contains(union, at))
			require.Equal(t, contains(groups[0], at) && !contains(groups[1], at), contains(difference, at))
		}
		require.Equal(t, union, scrape.Union(union))
		for _, set := range [][]models.SourceWindow{union, difference} {
			for i, w := range set {
				_, err := scrape.NormalizeWindow(w)
				require.NoError(t, err)
				if i > 0 {
					require.NotNil(t, w.Since)
					require.True(t, w.Since.After(set[i-1].Until))
				}
			}
		}
	}
	_, err := scrape.NormalizeWindow(models.SourceWindow{Since: &base, Until: base})
	require.ErrorIs(t, err, models.ErrSourceRunInvalid)
	_, err = scrape.NormalizeWindow(models.SourceWindow{Until: base.Add(time.Nanosecond)})
	require.ErrorIs(t, err, models.ErrSourceRunInvalid)
}

func TestSourceRunDestinationConfinesSymlinksAndMissingDirectories(t *testing.T) {
	base := t.TempDir()
	binding, err := archive.ProbeMediaRoot(base)
	require.NoError(t, err)
	root := &models.MediaRoot{MediaRootDefinition: models.MediaRootDefinition{State: "active", Binding: binding}}
	destination, prefix, err := scrape.Destination(root, "uncreated/album")
	require.NoError(t, err)
	require.Equal(t, filepath.ToSlash(filepath.Join(base, "uncreated/album")), destination)
	require.Equal(t, "uncreated/album", prefix)
	require.NoError(t, os.Mkdir(filepath.Join(base, "real"), 0700))
	require.NoError(t, os.Symlink("real", filepath.Join(base, "alias")))
	_, prefix, err = scrape.Destination(root, "alias/new")
	require.NoError(t, err)
	require.Equal(t, "real/new", prefix)
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(base, "outside")))
	_, _, err = scrape.Destination(root, "outside/new")
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
	require.NoError(t, os.Symlink("missing", filepath.Join(base, "dangling")))
	_, _, err = scrape.Destination(root, "dangling/new")
	require.Error(t, err)
	require.NoError(t, os.Rename(base, base+"-old"))
	t.Cleanup(func() { _ = os.RemoveAll(base + "-old") })
	require.NoError(t, os.Mkdir(base, 0700))
	_, _, err = scrape.Destination(root, "new")
	require.Error(t, err)
}
