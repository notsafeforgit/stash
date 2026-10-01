package manager

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/file"
	imagefile "github.com/stashapp/stash/pkg/file/image"
	"github.com/stashapp/stash/pkg/file/video"
	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
)

// NewIngestFileWorker connects native intake to the shared scanner, preview
// generators and after-success notifications. The API starts it only after the
// plugin GraphQL handler is registered, and stops it before closing SQLite.
func (s *Manager) NewIngestFileWorker(service *ingest.Service) *ingest.FileWorker {
	if !s.Config.GetEnableV3UI() || s.validateFFmpeg() != nil {
		return nil
	}
	scanner := func(kind models.ArchiveEntityKind) *file.Scanner {
		var decorator file.Decorator = &imagefile.Decorator{FFProbe: s.FFProbe}
		if kind == models.ArchiveScene {
			decorator = &video.Decorator{FFProbe: s.FFProbe}
		}
		return &file.Scanner{FingerprintCalculator: &fingerprintCalculator{s.Config}, FileDecorators: []file.Decorator{decorator}}
	}
	return ingest.NewFileWorker(service, scanner, s.finishFileIngestion)
}

func (s *Manager) finishFileIngestion(ctx context.Context, work ingest.FileWork, published ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
	if err := guard(ctx); err != nil {
		return err
	}
	var media *models.ArchiveEntity
	var scene *models.Scene
	var img *models.Image
	repo := s.Repository
	if err := repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		media, err = repo.ArchiveEntity.Resolve(ctx, published.MediaUUID)
		if err != nil {
			return err
		}
		if media == nil || media.State != models.ArchiveEntityActive || media.LocalID == nil {
			return models.ErrArchiveIdentityConflict
		}
		if media.Kind == models.ArchiveScene {
			scene, err = repo.Scene.Find(ctx, *media.LocalID)
			if err != nil {
				return err
			}
			if scene == nil {
				return models.ErrArchiveIdentityConflict
			}
			return scene.LoadPrimaryFile(ctx, repo.File)
		}
		img, err = repo.Image.Find(ctx, *media.LocalID)
		if err != nil {
			return err
		}
		if img == nil {
			return models.ErrArchiveIdentityConflict
		}
		return img.LoadPrimaryFile(ctx, repo.File)
	}); err != nil {
		return err
	}
	if scene != nil {
		// This preserves existing and concurrently edited cover selections.
		task := &GenerateCoverTask{repository: repo, Scene: *scene, publicationGuard: guard}
		if err := task.generateWithCoverSource(ctx); err != nil {
			return err
		}
	} else {
		task := &GenerateImageThumbnailTask{Image: *img}
		if task.required() {
			if err := s.generateImagePreview(ctx, img, guard); err != nil && !errors.Is(err, image.ErrNotSupportedForThumbnail) {
				return err
			}
		}
	}
	if err := guard(ctx); err != nil {
		return err
	}
	if s.PluginCache == nil {
		return nil
	}
	if published.MediaCreated || published.FileLinked {
		trigger := hook.ImageUpdatePost
		fields := []string{"files"}
		if media.Kind == models.ArchiveScene {
			trigger = hook.SceneUpdatePost
		}
		if published.MediaCreated {
			trigger, fields = hook.ImageCreatePost, nil
			if media.Kind == models.ArchiveScene {
				trigger = hook.SceneCreatePost
			}
		}
		if err := s.PluginCache.ExecuteDurablePostHooks(ctx, intakeHookEventID(work, published.MediaUUID, trigger), *media.LocalID, trigger, nil, fields); err != nil {
			return err
		}
	}
	if published.GalleryUUID != "" {
		if err := guard(ctx); err != nil {
			return err
		}
		var gallery *models.ArchiveEntity
		if err := repo.WithReadTxn(ctx, func(ctx context.Context) error {
			var err error
			gallery, err = repo.ArchiveEntity.Resolve(ctx, published.GalleryUUID)
			return err
		}); err != nil {
			return err
		}
		// A gallery removed after registration must not be recreated by retry.
		if gallery == nil || gallery.State != models.ArchiveEntityActive || gallery.LocalID == nil {
			return nil
		}
		trigger := hook.GalleryUpdatePost
		fields := []string{"image_ids", "scene_ids"}
		if published.Gallery == "create" {
			trigger, fields = hook.GalleryCreatePost, nil
		}
		return s.PluginCache.ExecuteDurablePostHooks(ctx, intakeHookEventID(work, published.GalleryUUID, trigger), *gallery.LocalID, trigger, nil, fields)
	}
	return nil
}

func intakeHookEventID(work ingest.FileWork, entity string, trigger hook.TriggerEnum) string {
	return uuid.NewSHA1(uuid.MustParse(work.Publication.UUID), []byte("hook\x00"+entity+"\x00"+trigger.String())).String()
}
