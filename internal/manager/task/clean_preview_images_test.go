package task

import (
	"context"
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

func TestCleanScenePreviewsReclaimsReplacedAVIFs(t *testing.T) {
	for _, kind := range []string{"cover", "marker"} {
		t.Run(kind, func(t *testing.T) {
			db := mocks.NewDatabase()
			p := paths.NewPaths(t.TempDir(), "")
			store := previewimage.Store{Root: p.Generated.PreviewImagesPath()}
			source := filepath.Join(t.TempDir(), "source.mp4")
			require.NoError(t, os.WriteFile(source, []byte("video"), 0600))
			scene := &models.Scene{ID: 1, Path: source, CoverChecksum: "current"}
			db.Scene.On("Find", mock.Anything, 1).Return(scene, nil)
			key := previewimage.CoverKey(scene.CoverChecksum)
			if kind == "marker" {
				db.SceneMarker.On("FindBySceneID", mock.Anything, 1).
					Return([]*models.SceneMarker{{SceneID: 1, Seconds: 1.5}}, nil)
				var err error
				key, err = previewimage.SourceKey(source, "1.5")
				require.NoError(t, err)
			}
			dir := filepath.Join(store.SceneDirectory(1), kind, key)
			var obsolete string
			for _, data := range []string{"old AVIF", "current AVIF"} {
				stage := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(stage, "preview.avif"), []byte(data), 0600))
				result := &previewimage.Result{Directory: stage, Variants: []previewimage.Variant{
					{File: "preview.avif", MIMEType: "image/avif", DynamicRange: previewimage.Adaptive, Width: 64, Height: 48},
				}}
				require.NoError(t, store.Publish(1, kind, key, 1.5, result))
				if data == "old AVIF" {
					m, err := store.Load(1, kind, key)
					require.NoError(t, err)
					obsolete = filepath.Join(dir, m.Variants[0].File)
				}
			}
			past := time.Now().Add(-2 * time.Hour)
			require.NoError(t, os.Chtimes(dir, past, past))
			before, err := store.Load(1, kind, key)
			require.NoError(t, err)
			j := CleanGeneratedJob{Repository: db.Repository(), Paths: &p, Options: CleanGeneratedOptions{DryRun: true}}
			require.NoError(t, j.cleanPreviewImages(context.Background(), kind))
			require.FileExists(t, obsolete)
			j.Options.DryRun = false
			require.NoError(t, j.cleanPreviewImages(context.Background(), kind))
			require.NoFileExists(t, obsolete)
			after, err := store.Load(1, kind, key)
			require.NoError(t, err)
			require.Equal(t, before, after, "cleanup must preserve the current catalog and its URLs")
		})
	}
}
