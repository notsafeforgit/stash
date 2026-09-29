package manager

import (
	"context"

	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/txn"
)

func (s *Manager) registerSceneCoverHook(ctx context.Context, id int) {
	if s.HookExecutor == nil {
		return
	}
	txn.AddPostCommitHook(ctx, func(ctx context.Context) {
		s.HookExecutor.ExecutePostHooks(ctx, id, hook.SceneUpdatePost, nil, []string{"cover_image"})
	})
}
