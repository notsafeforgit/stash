package api

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/models"
)

func (r *mutationResolver) entityImageDataFromScene(ctx context.Context, input models.SceneImageInput) ([]byte, error) {
	id, err := strconv.Atoi(input.ID)
	if err != nil {
		return nil, fmt.Errorf("converting scene id: %w", err)
	}
	if input.At != nil && (math.IsNaN(*input.At) || math.IsInf(*input.At, 0) || *input.At < 0) {
		return nil, fmt.Errorf("scene image time must be a finite, non-negative number")
	}

	var scene *models.Scene
	var data []byte
	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		scene, err = r.repository.Scene.Find(ctx, id)
		if err != nil {
			return err
		}
		if scene == nil {
			return fmt.Errorf("source scene %d not found", id)
		}
		if input.At == nil {
			data, err = r.repository.Scene.GetCover(ctx, id)
			return err
		}
		return scene.LoadPrimaryFile(ctx, r.repository.File)
	}); err != nil {
		return nil, err
	}

	if input.At != nil {
		file := scene.Files.Primary()
		if file == nil {
			return nil, fmt.Errorf("source scene %d has no primary file", id)
		}
		// Generation happens outside the database transaction and never publishes
		// scene artwork or touches the active streaming session.
		return manager.GetInstance().SceneFrameImage(ctx, file, *input.At)
	}
	if len(data) > 0 {
		return data, nil
	}
	// Match the screenshot endpoint for libraries with unmigrated covers.
	if scene.Path != "" {
		mgr := manager.GetInstance()
		hash := scene.GetHash(mgr.Config.GetVideoFileNamingAlgorithm())
		data, err = os.ReadFile(mgr.Paths.Scene.GetLegacyScreenshotPath(hash))
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading scene cover: %w", err)
		}
		if len(data) > 0 {
			return data, nil
		}
	}
	return nil, fmt.Errorf("source scene %d has no cover", id)
}
