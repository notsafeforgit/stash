package plugin

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
)

// WithEntityUpdateHooks connects shared domain edits to the same after-success
// hooks used by API edits. Domain state never depends on a plugin's response.
func WithEntityUpdateHooks(repo models.Repository, hooks FileHookExecutor) models.Repository {
	repo.TxnManager = models.WithEntityUpdateNotifier(repo.TxnManager, func(ctx context.Context, kind models.ArchiveEntityKind, id int, fields []string) {
		triggers := map[models.ArchiveEntityKind]hook.TriggerEnum{
			models.ArchivePerformer: hook.PerformerUpdatePost,
			models.ArchiveScene:     hook.SceneUpdatePost,
			models.ArchiveImage:     hook.ImageUpdatePost,
			models.ArchiveGallery:   hook.GalleryUpdatePost,
			models.ArchiveStudio:    hook.StudioUpdatePost,
			models.ArchiveTag:       hook.TagUpdatePost,
			models.ArchiveGroup:     hook.GroupUpdatePost,
			models.ArchiveFile:      hook.FileUpdatePost,
		}
		if trigger, ok := triggers[kind]; ok && hooks.HasHooks(trigger) {
			hooks.ExecutePostHooks(ctx, id, trigger, nil, fields)
		}
	})
	return repo
}
