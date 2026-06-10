package api

import (
	"context"
	"sort"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/plugin"
)

func (r *queryResolver) Plugins(ctx context.Context) ([]*plugin.Plugin, error) {
	return manager.GetInstance().PluginCache.ListPlugins(), nil
}

func (r *queryResolver) PluginTasks(ctx context.Context) ([]*plugin.PluginTask, error) {
	return manager.GetInstance().PluginCache.ListPluginTasks(), nil
}

func (r *queryResolver) PluginHookOrder(ctx context.Context) ([]*PluginHookOrder, error) {
	raw := config.GetInstance().GetPluginHookOrder()
	hooks := make([]string, 0, len(raw))
	for h := range raw {
		hooks = append(hooks, h)
	}
	sort.Strings(hooks)

	ret := make([]*PluginHookOrder, 0, len(hooks))
	for _, h := range hooks {
		ids := append([]string(nil), raw[h]...)
		ret = append(ret, &PluginHookOrder{
			Hook:      h,
			PluginIds: ids,
		})
	}
	return ret, nil
}
