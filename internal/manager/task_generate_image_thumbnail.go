package manager

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

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

func (t *GenerateImageThumbnailTask) logStderr(err error) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		logger.Debugf("[generator] error output: %s", exitErr.Stderr)
	}
}

func (t *GenerateImageThumbnailTask) Start(ctx context.Context) {
	if !t.required() {
		return
	}
	if GetInstance().Config.GetEnableV3UI() {
		err := GetInstance().GenerateImagePreview(ctx, &t.Image)
		if err != nil && ctx.Err() == nil && !errors.Is(err, image.ErrNotSupportedForThumbnail) {
			err = fmt.Errorf("image %d thumbnail: %w", t.Image.ID, err)
			logger.Error(err)
			if t.onError != nil {
				t.onError(err)
			}
		}
		return
	}

	thumbPath := GetInstance().Paths.Generated.GetThumbnailPath(t.Image.Checksum, models.DefaultGthumbWidth)
	f := t.Image.Files.Primary()
	if f == nil {
		return
	}
	path := f.Base().Path

	logger.Debugf("Generating thumbnail for %s", path)

	mgr := GetInstance()
	c := mgr.Config

	clipPreviewOptions := image.ClipPreviewOptions{
		InputArgs:  c.GetTranscodeInputArgs(),
		OutputArgs: c.GetTranscodeOutputArgs(),
		Preset:     c.GetPreviewPreset().String(),
	}

	encoder := image.NewThumbnailEncoder(mgr.FFMpeg, mgr.FFProbe, clipPreviewOptions)
	data, err := encoder.GetThumbnail(f, models.DefaultGthumbWidth)

	if err != nil {
		// don't log for animated images
		if !errors.Is(err, image.ErrNotSupportedForThumbnail) {
			logger.Errorf("[generator] getting thumbnail for image %s: %s", path, err.Error())
			t.logStderr(err)
		}
		return
	}

	err = fsutil.WriteFile(thumbPath, data)
	if err != nil {
		logger.Errorf("[generator] writing thumbnail for image %s: %s", path, err.Error())
		return
	}
}

func (t *GenerateImageThumbnailTask) required() bool {
	f := t.Image.Files.Primary()
	vf, ok := f.(models.VisualFile)
	if !ok {
		return false
	}
	if GetInstance().Config.GetEnableV3UI() {
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

	if vf.GetHeight() <= models.DefaultGthumbWidth && vf.GetWidth() <= models.DefaultGthumbWidth {
		return false
	}

	if t.Overwrite {
		return true
	}

	thumbPath := GetInstance().Paths.Generated.GetThumbnailPath(t.Image.Checksum, models.DefaultGthumbWidth)
	exists, _ := fsutil.FileExists(thumbPath)

	return !exists
}
