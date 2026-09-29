package api

import (
	"context"

	"github.com/stashapp/stash/pkg/plugin/hook"
)

// Specialized mutations have no entity-update input. List the affected fields
// and let plugins query the committed values, just as scan notifications do.
func (r *mutationResolver) sceneFieldsUpdated(ctx context.Context, id int, fields ...string) {
	if len(fields) > 0 {
		r.hookExecutor.ExecutePostHooks(ctx, id, hook.SceneUpdatePost, nil, fields)
	}
}

func (r *mutationResolver) sceneActivityUpdated(ctx context.Context, id int, resume, duration bool) {
	var fields []string
	if resume {
		fields = append(fields, "resume_time")
	}
	if duration {
		fields = append(fields, "play_duration")
	}
	r.sceneFieldsUpdated(ctx, id, fields...)
}

func (r *mutationResolver) imageFieldsUpdated(ctx context.Context, id int, fields ...string) {
	r.hookExecutor.ExecutePostHooks(ctx, id, hook.ImageUpdatePost, nil, fields)
}

func (r *mutationResolver) galleryFieldsUpdated(ctx context.Context, id int, fields ...string) {
	r.hookExecutor.ExecutePostHooks(ctx, id, hook.GalleryUpdatePost, nil, fields)
}

func (r *mutationResolver) groupFieldsUpdated(ctx context.Context, id int, fields ...string) {
	r.hookExecutor.ExecutePostHooks(ctx, id, hook.GroupUpdatePost, nil, fields)
	r.hookExecutor.ExecutePostHooks(ctx, id, hook.MovieUpdatePost, nil, fields)
}
