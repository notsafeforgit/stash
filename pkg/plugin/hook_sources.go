package plugin

import (
	"context"

	"github.com/stashapp/stash/pkg/plugin/common"
	"github.com/stashapp/stash/pkg/session"
)

func parentHookSources(ctx context.Context) []common.HookSource {
	visited := session.GetVisitedPluginHooks(ctx)
	ret := make([]common.HookSource, 0, len(visited))
	for _, source := range visited {
		ret = append(ret, common.HookSource{PluginID: source.PluginID, Type: source.HookType.String()})
	}
	return ret
}
