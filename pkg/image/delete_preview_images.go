package image

import (
	"os"

	"github.com/stashapp/stash/pkg/previewimage"
)

func (d *FileDeleter) markPreviewImages(imageID int) error {
	store := previewimage.Store{Root: d.Paths.Generated.PreviewImagesPath()}
	dir := store.ImageDirectory(imageID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return d.DirsWithoutTrash([]string{dir})
}
