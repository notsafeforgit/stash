package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/previewimage"
)

// Clean the standalone preview store along with the corresponding legacy
// category. Recent entries are left alone so an in-flight generation/cover
// transaction cannot have its just-published files removed by maintenance.
func (j *CleanGeneratedJob) cleanPreviewImages(ctx context.Context, kind string) error {
	store := previewimage.Store{Root: j.Paths.Generated.PreviewImagesPath()}
	root := filepath.Join(store.Root, strconv.Itoa(previewimage.RecipeVersion))
	scenes, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range scenes {
		if err := ctx.Err(); err != nil {
			return err
		}
		id, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		var scene *models.Scene
		var markers []*models.SceneMarker
		if err := j.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
			var err error
			scene, err = j.Repository.Scene.Find(ctx, id)
			if err != nil || scene == nil || kind != "marker" {
				return err
			}
			markers, err = j.Repository.SceneMarker.FindBySceneID(ctx, id)
			return err
		}); err != nil {
			return err
		}
		keep := map[string]bool{}
		if scene != nil {
			// An offline source cannot be checked reliably. Keep its previews.
			if _, err := os.Stat(scene.Path); err != nil {
				continue
			}
			if kind == "cover" && scene.CoverChecksum != "" {
				key, err := previewimage.SourceKey(scene.Path, scene.CoverChecksum)
				if err != nil {
					return err
				}
				keep[key] = true
			}
			for _, marker := range markers {
				key, err := previewimage.SourceKey(scene.Path, strconv.FormatFloat(marker.Seconds, 'f', -1, 64))
				if err != nil {
					return err
				}
				keep[key] = true
			}
		}
		parent := filepath.Join(store.SceneDirectory(id), kind)
		entries, err := os.ReadDir(parent)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, cached := range entries {
			info, err := cached.Info()
			if err != nil {
				return fmt.Errorf("reading preview cache: %w", err)
			}
			if cached.IsDir() && !keep[cached.Name()] && time.Since(info.ModTime()) > time.Hour {
				j.deleteDir(filepath.Join(parent, cached.Name()))
			}
		}
	}
	return nil
}
