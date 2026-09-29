package task

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/previewimage"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCleanImagePreviewsRespectsCurrentOfflineAndDryRun(t *testing.T) {
	db := mocks.NewDatabase()
	p := paths.NewPaths(t.TempDir(), "")
	store := previewimage.Store{Root: p.Generated.PreviewImagesPath()}
	online := filepath.Join(t.TempDir(), "original.png")
	require.NoError(t, os.WriteFile(online, []byte("original"), 0600))
	db.Image.On("Find", mock.Anything, 1).Return(&models.Image{ID: 1, Path: online, Checksum: "current"}, nil)
	db.Image.On("Find", mock.Anything, 2).Return((*models.Image)(nil), nil)
	db.Image.On("Find", mock.Anything, 3).Return(&models.Image{ID: 3, Path: "/missing/photo.png", Checksum: "offline"}, nil)
	current := filepath.Join(store.ImageDirectory(1), "image", previewimage.ImageKey("current"))
	obsolete := filepath.Join(store.ImageDirectory(1), "image", previewimage.ImageKey("obsolete"))
	orphan := filepath.Join(store.ImageDirectory(2), "image", previewimage.ImageKey("orphan"))
	offline := filepath.Join(store.ImageDirectory(3), "image", previewimage.ImageKey("offline"))
	recent := filepath.Join(store.ImageDirectory(1), "image", previewimage.ImageKey("in-flight"))
	scene := filepath.Join(store.SceneDirectory(1), "cover", previewimage.CoverKey("cover"))
	jpegName := fmt.Sprintf("%x.jpg", sha256.Sum256([]byte("redundant JPEG")))
	avifName := fmt.Sprintf("%x.avif", sha256.Sum256([]byte("obsolete AVIF")))
	for _, fixture := range []struct {
		id  int
		key string
		dir string
	}{{1, "current", current}, {3, "offline", offline}, {1, "in-flight", recent}} {
		stage := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(stage, "preview.avif"), []byte("cached AVIF"), 0600))
		result := &previewimage.Result{Directory: stage, Variants: []previewimage.Variant{{File: "preview.avif", MIMEType: "image/avif", DynamicRange: previewimage.SDR, Width: 64, Height: 48}}}
		require.NoError(t, store.Publish(fixture.id, "image", previewimage.ImageKey(fixture.key), 0, result))
		require.NoError(t, os.WriteFile(filepath.Join(fixture.dir, jpegName), []byte("redundant JPEG"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(fixture.dir, avifName), []byte("obsolete AVIF"), 0600))
	}
	for _, dir := range []string{current, obsolete, orphan, offline, recent, scene} {
		require.NoError(t, os.MkdirAll(dir, 0755))
		if dir != recent {
			past := time.Now().Add(-2 * time.Hour)
			require.NoError(t, os.Chtimes(dir, past, past))
		}
	}
	j := CleanGeneratedJob{Repository: db.Repository(), Paths: &p, Options: CleanGeneratedOptions{DryRun: true}}
	require.NoError(t, j.cleanImagePreviewImages(context.Background()))
	require.DirExists(t, obsolete)
	require.DirExists(t, orphan)
	require.FileExists(t, filepath.Join(current, jpegName), "dry run cannot reclaim JPEGs")
	require.FileExists(t, filepath.Join(current, avifName), "dry run cannot reclaim obsolete AVIFs")
	j.Options.DryRun = false
	require.NoError(t, j.cleanImagePreviewImages(context.Background()))
	require.NoDirExists(t, obsolete)
	require.NoDirExists(t, orphan)
	for _, dir := range []string{current, offline, recent, scene} {
		require.DirExists(t, dir)
	}
	require.NoFileExists(t, filepath.Join(current, jpegName))
	require.FileExists(t, filepath.Join(offline, jpegName))
	require.FileExists(t, filepath.Join(recent, jpegName))
	require.NoFileExists(t, filepath.Join(current, avifName))
	require.FileExists(t, filepath.Join(offline, avifName))
	require.FileExists(t, filepath.Join(recent, avifName))
	_, err := store.Load(1, "image", previewimage.ImageKey("current"))
	require.NoError(t, err, "cleaning a current cache must retain the usable rendition")
}
