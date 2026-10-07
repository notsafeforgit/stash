package manager

import (
	"context"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
)

// Source album publication uses library metadata only; it does not require
// FFmpeg, a filesystem grant or a producer credential.
func (s *Manager) NewAlbumBackfillWorker(service *gallery.AlbumBackfill) *gallery.AlbumWorker {
	return gallery.NewAlbumWorker(service, s.finishAlbumBackfill)
}

func (s *Manager) NewPostMergeNotificationWorker(service *gallery.PostMergeNotifications) *gallery.PostMergeNotificationWorker {
	return gallery.NewPostMergeNotificationWorker(service, func(ctx context.Context, review models.PostConsolidationReview, guard gallery.AlbumEffectGuard) error {
		result := review.Result.Gallery
		return s.finishAlbumBackfill(ctx, gallery.AlbumPublication{EventUUID: review.Request.RequestUUID,
			PostUUID: review.Request.DestinationUUID, GalleryUUID: result.GalleryUUID, Action: result.Action,
			Created: result.Created, Added: len(result.Added), Removed: len(result.Removed)}, guard)
	})
}

func (s *Manager) finishAlbumBackfill(ctx context.Context, published gallery.AlbumPublication, guard gallery.AlbumEffectGuard) error {
	if err := guard(ctx); err != nil {
		return err
	}
	if !published.ChangedGallery() || s.PluginCache == nil {
		return nil
	}
	var entity *models.ArchiveEntity
	if err := s.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		entity, err = s.Repository.ArchiveEntity.Resolve(ctx, published.GalleryUUID)
		return err
	}); err != nil {
		return err
	}
	// Never target a replacement that happens to reuse a deleted local ID.
	if entity == nil || entity.State != models.ArchiveEntityActive || entity.LocalID == nil {
		return nil
	}
	if entity.Kind != models.ArchiveGallery {
		return gallery.ErrAlbumWorkInvalid
	}
	trigger, fields := hook.GalleryUpdatePost, []string{"image_ids", "scene_ids"}
	if published.Created {
		trigger, fields = hook.GalleryCreatePost, nil
	}
	if err := guard(ctx); err != nil {
		return err
	}
	event := uuid.NewSHA1(uuid.MustParse(published.EventUUID), []byte("hook\x00"+published.GalleryUUID+"\x00"+trigger.String())).String()
	return s.PluginCache.ExecuteDurablePostHooks(ctx, event, *entity.LocalID, trigger, nil, fields)
}
