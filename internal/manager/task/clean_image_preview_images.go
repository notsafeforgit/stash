package task

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/previewimage"
)

func (j *CleanGeneratedJob) cleanImagePreviewImages(ctx context.Context) error {
	store := previewimage.Store{Root: j.Paths.Generated.PreviewImagesPath()}
	root := filepath.Dir(store.ImageDirectory(1))
	images, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range images {
		if err := ctx.Err(); err != nil {
			return err
		}
		id, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		var img *models.Image
		if err := j.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
			var err error
			img, err = j.Repository.Image.Find(ctx, id)
			return err
		}); err != nil {
			return err
		}
		key := ""
		if img != nil {
			// Match the scene cache's conservative policy for offline originals.
			if _, err := os.Stat(img.Path); err != nil {
				continue
			}
			if img.Checksum != "" {
				key = previewimage.ImageKey(img.Checksum)
			}
		}
		parent := filepath.Join(store.ImageDirectory(id), "image")
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
				return err
			}
			if cached.IsDir() && time.Since(info.ModTime()) > time.Hour {
				if cached.Name() == key {
					j.compactPreviewImages(store, id, "image", cached.Name())
				} else {
					j.deleteDir(filepath.Join(parent, cached.Name()))
				}
			}
		}
	}
	return nil
}
