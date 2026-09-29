package plugin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/pkg/plugin/common"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/session"
	"github.com/stretchr/testify/require"
)

func TestParentHookSources(t *testing.T) {
	ctx := context.Background()
	require.Empty(t, parentHookSources(ctx))
	ctx = session.AddVisitedPluginHook(ctx, "titleFromFilename", hook.SceneCreatePost)
	ctx = session.AddVisitedPluginHook(ctx, "other", hook.SceneUpdatePost)
	encoded, err := json.Marshal(common.HookContext{ParentHooks: parentHookSources(ctx), Type: hook.SceneUpdatePost.String()})
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"Scene.Update.Post","input":null,"parentHooks":[{"pluginId":"titleFromFilename","type":"Scene.Create.Post"},{"pluginId":"other","type":"Scene.Update.Post"}]}`, string(encoded))
}
