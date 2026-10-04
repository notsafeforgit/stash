package manager

import (
	"context"
	"errors"
	"fmt"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
)

type GenerateImageThumbnailTask struct {
	Image     models.Image
	Overwrite bool
	onError   func(error)
}

func (t *GenerateImageThumbnailTask) GetDescription() string {
	return fmt.Sprintf("Generating Thumbnail for image %s", t.Image.Path)
}

func (t *GenerateImageThumbnailTask) Start(ctx context.Context) {
	if !t.required() {
		return
	}
	err := GetInstance().GenerateImagePreview(ctx, &t.Image)
	if err != nil && ctx.Err() == nil && !errors.Is(err, image.ErrNotSupportedForThumbnail) {
		err = fmt.Errorf("image %d thumbnail: %w", t.Image.ID, err)
		logger.Error(err)
		if t.onError != nil {
			t.onError(err)
		}
	}
}

func (t *GenerateImageThumbnailTask) required() bool {
	f := t.Image.Files.Primary()
	_, ok := f.(models.VisualFile)
	if !ok {
		return false
	}
	if im, ok := f.(*models.ImageFile); ok && im.Format == "gif" {
		return false
	}
	if t.Overwrite || GetInstance().ImagePreviewImage(&t.Image) == nil {
		return true
	}
	path := GetInstance().Paths.Generated.GetThumbnailPath(t.Image.Checksum, models.DefaultGthumbWidth)
	exists, _ := fsutil.FileExists(path)
	return !exists
}
