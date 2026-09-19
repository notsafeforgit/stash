package manager

import (
	"context"
	"fmt"
	"math"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scene/generate"
)

// SceneFrameImage extracts a standalone image without updating the scene's
// cover, preview files, timestamps or active streams.
func (s *Manager) SceneFrameImage(ctx context.Context, file *models.VideoFile, at float64) ([]byte, error) {
	if math.IsNaN(at) || math.IsInf(at, 0) || at < 0 || at >= file.Duration {
		return nil, fmt.Errorf("scene image time must be within the video duration")
	}
	if err := s.Paths.Generated.EnsureTmpDir(); err != nil {
		return nil, fmt.Errorf("preparing scene image generation: %w", err)
	}
	generator := generate.Generator{
		Encoder:      s.FFMpeg,
		FFMpegConfig: s.Config,
		LockManager:  s.ReadLockManager,
		ScenePaths:   s.Paths.Scene,
	}
	return generator.Screenshot(ctx, file.Path, file.Width, file.Duration, generate.ScreenshotOptions{At: &at})
}
