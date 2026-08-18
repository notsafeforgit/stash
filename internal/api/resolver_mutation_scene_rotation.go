package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/file"
	filevideo "github.com/stashapp/stash/pkg/file/video"
	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stashapp/stash/pkg/hash/oshash"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/scene"
)

// SceneVideoRotate changes the primary MKV's first video-stream ROTATE tag,
// then updates the file row and every scene that shares the file. The remux is
// staged before the write transaction and keeps an on-disk backup until the
// transaction commits, so a database failure can restore the original video.
func (r *mutationResolver) SceneVideoRotate(ctx context.Context, input SceneVideoRotateInput) (*models.Scene, error) {
	sceneID, err := strconv.Atoi(input.ID)
	if err != nil {
		return nil, fmt.Errorf("converting id: %w", err)
	}
	direction, err := sceneVideoRotationDirectionFromGraphQL(input.Direction)
	if err != nil {
		return nil, err
	}

	var targetScene *models.Scene
	var primaryFile *models.VideoFile
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		targetScene, err = r.repository.Scene.Find(ctx, sceneID)
		if err != nil {
			return err
		}
		if targetScene == nil {
			return fmt.Errorf("scene with id %d not found", sceneID)
		}
		if err := targetScene.LoadPrimaryFile(ctx, r.repository.File); err != nil {
			return fmt.Errorf("loading primary file: %w", err)
		}
		primaryFile = targetScene.Files.Primary()
		if primaryFile == nil {
			return fmt.Errorf("scene %d has no primary video file", sceneID)
		}
		if primaryFile.ZipFileID != nil {
			return errors.New("rotating zip-contained videos is not supported")
		}
		return nil
	}); err != nil {
		return nil, err
	}

	mgr := manager.GetInstance()
	fileNamingAlgo := mgr.Config.GetVideoFileNamingAlgorithm()
	if mgr.StreamManager != nil {
		mgr.StreamManager.StopV3StreamsForFile(primaryFile.ID, "")
	}
	manager.KillRunningStreams(targetScene, fileNamingAlgo)

	rotationPatch, err := filevideo.StageRotationMetadata(
		ctx,
		mgr.FFMpeg,
		mgr.FFProbe,
		primaryFile.Path,
		direction,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rotationPatch.Rollback(); err != nil {
			logger.Errorf("rolling back scene video rotation: %v", err)
		}
	}()

	rotatedFile, err := prepareRotatedVideoFile(ctx, mgr, primaryFile, rotationPatch.StagedPath)
	if err != nil {
		return nil, err
	}

	fileDeleter := &scene.FileDeleter{
		Deleter:        file.NewDeleter(),
		FileNamingAlgo: fileNamingAlgo,
		Paths:          mgr.Paths,
	}
	var impactedScenes []*models.Scene
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		currentScene, err := r.repository.Scene.Find(ctx, sceneID)
		if err != nil {
			return err
		}
		if currentScene == nil {
			return fmt.Errorf("scene with id %d no longer exists", sceneID)
		}
		if err := currentScene.LoadPrimaryFile(ctx, r.repository.File); err != nil {
			return fmt.Errorf("reloading primary file: %w", err)
		}
		currentPrimary := currentScene.Files.Primary()
		if currentPrimary == nil || currentPrimary.ID != primaryFile.ID {
			return errors.New("scene primary file changed while rotation metadata was being prepared")
		}

		impactedScenes, err = r.repository.Scene.FindByFileID(ctx, primaryFile.ID)
		if err != nil {
			return fmt.Errorf("finding scenes for rotated file: %w", err)
		}
		for _, impacted := range impactedScenes {
			if err := fileDeleter.MarkGeneratedFiles(impacted); err != nil {
				return fmt.Errorf("marking generated files for scene %d: %w", impacted.ID, err)
			}
		}

		if err := rotationPatch.Apply(); err != nil {
			return err
		}
		if err := r.repository.File.Update(ctx, rotatedFile); err != nil {
			return fmt.Errorf("updating rotated video file row: %w", err)
		}
		for _, impacted := range impactedScenes {
			if _, err := r.repository.Scene.UpdatePartial(ctx, impacted.ID, models.NewScenePartial()); err != nil {
				return fmt.Errorf("updating scene %d after video rotation: %w", impacted.ID, err)
			}
		}
		return nil
	}); err != nil {
		fileDeleter.Rollback()
		return nil, err
	}

	manager.InvalidateVideoProbeCaches(primaryFile.Path)
	fileDeleter.Commit()
	if err := rotationPatch.Commit(); err != nil {
		// The database and installed file are already committed. Leaving the
		// hidden original beside it is recoverable and safer than reporting a
		// failed mutation whose visible result actually succeeded.
		logger.Warnf("scene video rotation succeeded but cleanup failed: %v", err)
	}

	for _, impacted := range impactedScenes {
		r.hookExecutor.ExecutePostHooks(ctx, impacted.ID, hook.SceneUpdatePost, input, []string{"files"})
	}

	return r.getScene(ctx, sceneID)
}

func prepareRotatedVideoFile(
	ctx context.Context,
	mgr *manager.Manager,
	original *models.VideoFile,
	stagedPath string,
) (*models.VideoFile, error) {
	candidate := original.Clone().(*models.VideoFile)
	candidate.Path = stagedPath

	decorated, err := (&filevideo.Decorator{FFProbe: mgr.FFProbe}).Decorate(
		ctx,
		&file.OsFS{},
		candidate,
	)
	if err != nil {
		return nil, fmt.Errorf("reading rotated video metadata: %w", err)
	}
	rotated, ok := decorated.(*models.VideoFile)
	if !ok {
		return nil, fmt.Errorf("rotated file metadata has unexpected type %T", decorated)
	}

	info, err := os.Stat(stagedPath)
	if err != nil {
		return nil, fmt.Errorf("statting staged rotated video: %w", err)
	}

	newOSHash, err := oshash.FromFilePath(stagedPath)
	if err != nil {
		return nil, fmt.Errorf("recomputing oshash: %w", err)
	}
	fingerprints := models.Fingerprints{{
		Type:        models.FingerprintTypeOshash,
		Fingerprint: newOSHash,
	}}
	if mgr.Config.IsCalculateMD5() {
		newMD5, err := md5.FromFilePath(stagedPath)
		if err != nil {
			return nil, fmt.Errorf("recomputing MD5: %w", err)
		}
		fingerprints = append(fingerprints, models.Fingerprint{
			Type:        models.FingerprintTypeMD5,
			Fingerprint: newMD5,
		})
	}

	base := rotated.Base()
	base.Path = original.Path
	base.Size = info.Size()
	base.ModTime = info.ModTime()
	base.UpdatedAt = time.Now()
	base.Fingerprints = fingerprints
	return rotated, nil
}

func sceneVideoRotationDirectionFromGraphQL(direction SceneVideoRotationDirection) (filevideo.RotationDirection, error) {
	switch direction {
	case SceneVideoRotationDirectionCw:
		return filevideo.RotateCW, nil
	case SceneVideoRotationDirectionCcw:
		return filevideo.RotateCCW, nil
	case SceneVideoRotationDirectionClear:
		return filevideo.RotateClear, nil
	default:
		return 0, errors.New("unknown scene video rotation direction")
	}
}
