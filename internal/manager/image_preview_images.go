package manager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/previewimage"
)

func (s *Manager) ImagePreviewImage(img *models.Image) *previewimage.Manifest {
	if !s.Config.GetEnableV3UI() || img.Checksum == "" {
		return nil
	}
	m, err := s.PreviewImageStore().Load(img.ID, "image", previewimage.ImageKey(img.Checksum))
	if err != nil {
		return nil
	}
	return m
}

// GenerateImagePreview is shared by batch generation, scans and missing-cache
// thumbnail requests. The legacy URL remains a JPEG, made from the same SDR
// rendition; v3 gets the additive, display-adaptive rendition catalog.
func (s *Manager) GenerateImagePreview(ctx context.Context, img *models.Image) error {
	return s.generateImagePreview(ctx, img, nil)
}

func (s *Manager) generateImagePreview(ctx context.Context, img *models.Image, guard func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f := img.Files.Primary()
	if f == nil || img.Checksum == "" {
		return fmt.Errorf("image %d has no primary file or checksum", img.ID)
	}
	still, isStill := f.(*models.ImageFile)
	if isStill && strings.EqualFold(still.Format, "gif") {
		return image.ErrNotSupportedForThumbnail
	}
	source := f.Base().Path
	if f.Base().ZipFile != nil {
		source = f.Base().ZipFile.Base().Path
	}
	lock := s.ReadLockManager.ReadLock(ctx, source)
	defer lock.Cancel()
	before, err := previewimage.SourceKey(source, "")
	if err != nil {
		return err
	}
	path := f.Base().Path
	store := s.PreviewImageStore()
	if f.Base().ZipFile != nil {
		if err := os.MkdirAll(store.Root, 0755); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(store.Root, ".image-source-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		path = filepath.Join(stage, "source"+filepath.Ext(path))
		input, err := f.Open(&file.OsFS{})
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.Create(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return err
		}
	}
	if isStill && strings.EqualFold(still.Format, "webp") {
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		header := make([]byte, 48)
		n, readErr := input.Read(header)
		_ = input.Close()
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		if image.IsAnimatedWebP(header[:n]) {
			return image.ErrNotSupportedForThumbnail
		}
	}
	video, err := s.FFProbe.NewVideoFile(path)
	if err != nil {
		return err
	}
	if isStill && (video.FrameCount > 1 || video.Container == "apng") {
		return image.ErrNotSupportedForThumbnail
	}
	gainTool, _ := exec.LookPath("avifgainmaputil")
	avifTool, _ := exec.LookPath("avifenc")
	encoder := previewimage.Encoder{FFmpeg: s.FFMpeg, GainMapTool: gainTool, AVIFTool: avifTool, VIPSTool: image.GetVipsPath()}
	result, err := encoder.Generate(lock, store.Root, previewimage.Request{Video: video, StillImage: isStill, MaxDimension: models.DefaultGthumbWidth})
	if err != nil {
		return err
	}
	defer result.Close()
	hasAVIF := false
	for _, variant := range result.Variants {
		hasAVIF = hasAVIF || variant.MIMEType == "image/avif"
	}
	if !hasAVIF {
		return errors.Join(fmt.Errorf("AVIF image thumbnail was not generated"), errors.Join(result.Warnings...))
	}
	if after, err := previewimage.SourceKey(source, ""); err != nil || before != after {
		return fmt.Errorf("image source changed during thumbnail generation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		logger.Warnf("[preview image] image %d: %v", img.ID, warning)
	}
	data, err := result.JPEG()
	if err != nil {
		return err
	}
	if guard != nil {
		if err := guard(ctx); err != nil {
			return err
		}
	}
	if err := store.Publish(img.ID, "image", previewimage.ImageKey(img.Checksum), 0, result); err != nil {
		return err
	}
	return writeImageThumbnail(s.Paths.Generated.GetThumbnailPath(img.Checksum, models.DefaultGthumbWidth), data)
}

func writeImageThumbnail(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".thumbnail-*.jpg")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

type imageGenerationFailures struct{ coverGenerationFailures }

func (r *imageGenerationFailures) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.count == 0 {
		return nil
	}
	return fmt.Errorf("%d image thumbnails could not be generated; check the task log for image IDs. First error: %w", r.count, r.first)
}
