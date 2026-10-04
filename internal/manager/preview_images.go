package manager

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/previewimage"
	"github.com/stashapp/stash/pkg/scene/generate"
)

func (s *Manager) PreviewImageStore() previewimage.Store {
	return previewimage.Store{Root: s.Paths.Generated.PreviewImagesPath()}
}

func MarkerPreviewIdentity(seconds float64) string {
	return strconv.FormatFloat(seconds, 'f', -1, 64)
}

func (s *Manager) ScenePreviewImage(scene *models.Scene) *previewimage.Manifest {
	if scene.CoverChecksum == "" {
		return nil
	}
	if m, err := s.PreviewImageStore().Load(scene.ID, "cover", previewimage.CoverKey(scene.CoverChecksum)); err == nil {
		return m
	}
	return s.loadPreviewImage(scene, "cover", scene.CoverChecksum)
}

func (s *Manager) MarkerPreviewImage(scene *models.Scene, marker *models.SceneMarker) *previewimage.Manifest {
	if marker.SceneID != scene.ID {
		return nil
	}
	return s.loadPreviewImage(scene, "marker", MarkerPreviewIdentity(marker.Seconds))
}

func (s *Manager) loadPreviewImage(scene *models.Scene, kind, identity string) *previewimage.Manifest {
	revision, err := previewimage.SourceKey(scene.Path, identity)
	if err != nil {
		return nil
	}
	m, err := s.PreviewImageStore().Load(scene.ID, kind, revision)
	if err != nil {
		return nil
	}
	return m
}

func (s *Manager) generatePreviewImage(ctx context.Context, scene *models.Scene, kind, identity string, at float64) ([]byte, error) {
	lock := s.ReadLockManager.ReadLock(ctx, scene.Path)
	defer lock.Cancel()
	sourceRevision, err := previewimage.SourceKey(scene.Path, "")
	if err != nil {
		return nil, err
	}
	video, err := s.FFProbe.NewVideoFile(scene.Path)
	if err != nil {
		return nil, err
	}
	// Probe instead of relying on optional metadata from an earlier scan.
	tool, _ := exec.LookPath("avifgainmaputil")
	avifTool, _ := exec.LookPath("avifenc")
	encoder := previewimage.Encoder{FFmpeg: s.FFMpeg, GainMapTool: tool, AVIFTool: avifTool}
	store := s.PreviewImageStore()
	req := previewimage.Request{Video: video, At: at}
	if kind == "cover" {
		req.ThumbnailMaxDimension = previewimage.CardThumbnailMaxDimension
	}
	result, err := encoder.Generate(lock, store.Root, req)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	if current, err := previewimage.SourceKey(scene.Path, ""); err != nil || current != sourceRevision {
		return nil, fmt.Errorf("source changed during preview generation")
	}
	for _, warning := range result.Warnings {
		logger.Warnf("[preview image] %v", warning)
	}
	data, err := result.JPEG()
	if err != nil {
		return nil, err
	}
	var revision string
	if kind == "cover" {
		revision = previewimage.CoverKey(md5.FromBytes(data))
	} else {
		revision, err = previewimage.SourceKey(scene.Path, identity)
		if err != nil {
			return nil, err
		}
	}
	if err := store.Publish(scene.ID, kind, revision, at, result); err != nil {
		return nil, err
	}
	return data, nil
}

// Persist the SDR rendition for the public JPEG endpoint alongside the preview
// manifest. A plain screenshot remains available if richer encoding fails.
func (s *Manager) generateCoverImage(ctx context.Context, scene *models.Scene, file *models.VideoFile, at float64, fallback generate.Generator) ([]byte, error) {
	data, err := s.generatePreviewImage(ctx, scene, "cover", "", at)
	if err == nil {
		return data, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	logger.Warnf("[preview image] scene %d: %v; using JPEG screenshot generator", scene.ID, err)
	return fallback.Screenshot(ctx, file.Path, file.Width, file.Duration, generate.ScreenshotOptions{At: &at})
}

func (t *GenerateMarkersTask) generateMarkerScreenshot(ctx context.Context, scene *models.Scene, marker *models.SceneMarker, videoFile *models.VideoFile) error {
	s := instance
	hash := scene.GetHash(t.fileNamingAlgorithm)
	output := s.Paths.SceneMarkers.GetScreenshotPath(hash, int(marker.Seconds))
	jpegExists, _ := fsutil.FileExists(output)
	if !t.Overwrite && jpegExists && s.MarkerPreviewImage(scene, marker) != nil {
		return nil
	}
	data, err := s.generatePreviewImage(ctx, scene, "marker", MarkerPreviewIdentity(marker.Seconds), marker.Seconds)
	if err == nil {
		// Stage beside the destination so publication remains atomic even
		// when generated directories are mounted on separate filesystems.
		if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(output), ".preview-*.jpg")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		_, writeErr := tmp.Write(data)
		closeErr := tmp.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		return os.Rename(tmp.Name(), output)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	logger.Warnf("[preview image] marker %d: %v; using JPEG screenshot generator", marker.ID, err)
	if t.generator == nil {
		return fmt.Errorf("missing marker generator")
	}
	return t.generator.SceneMarkerScreenshot(ctx, videoFile.Path, hash, marker.Seconds, videoFile.Width)
}
