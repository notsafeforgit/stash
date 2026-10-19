package api

import (
	"context"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/plugin"
	"github.com/stashapp/stash/pkg/sliceutil"
)

func toPluginArgs(args []*plugin.PluginArgInput) plugin.OperationInput {
	ret := make(plugin.OperationInput)
	for _, a := range args {
		ret[a.Key] = toPluginArgValue(a.Value)
	}

	return ret
}

func toPluginArgValue(arg *plugin.PluginValueInput) interface{} {
	if arg == nil {
		return nil
	}

	switch {
	case arg.Str != nil:
		return *arg.Str
	case arg.I != nil:
		return *arg.I
	case arg.B != nil:
		return *arg.B
	case arg.F != nil:
		return *arg.F
	case arg.O != nil:
		return toPluginArgs(arg.O)
	case arg.A != nil:
		var ret []interface{}
		for _, v := range arg.A {
			ret = append(ret, toPluginArgValue(v))
		}
		return ret
	}

	return nil
}

func (r *mutationResolver) RunPluginTask(
	ctx context.Context,
	pluginID string,
	taskName *string,
	description *string,
	args []*plugin.PluginArgInput,
	argsMap map[string]interface{},
) (string, error) {
	if argsMap == nil {
		// convert args to map
		// otherwise ignore args in favour of args map
		argsMap = toPluginArgs(args)
	}

	m := manager.GetInstance()
	jobID, err := m.RunPluginTask(ctx, pluginID, taskName, description, argsMap)
	return jobID, err
}

func (r *mutationResolver) RunPluginOperation(
	ctx context.Context,
	pluginID string,
	args map[string]interface{},
) (interface{}, error) {
	if args == nil {
		args = make(map[string]interface{})
	}

	m := manager.GetInstance()
	return m.PluginCache.RunPlugin(ctx, pluginID, args)
}

func (r *mutationResolver) ReloadPlugins(ctx context.Context) (bool, error) {
	manager.GetInstance().RefreshPluginCache()
	return true, nil
}

func (r *mutationResolver) SetPluginsEnabled(ctx context.Context, enabledMap map[string]bool) (bool, error) {
	err := r.settings.update(ctx, func(c *config.Config) (func(), error) {

		existingDisabled := c.GetDisabledPlugins()
		var newDisabled []string

		// remove plugins that are no longer disabled
		for _, disabledID := range existingDisabled {
			if enabled, found := enabledMap[disabledID]; !enabled || !found {
				newDisabled = append(newDisabled, disabledID)
			}
		}

		// add plugins that are newly disabled
		for pluginID, enabled := range enabledMap {
			if !enabled {
				newDisabled = sliceutil.AppendUnique(newDisabled, pluginID)
			}
		}

		c.SetInterface(config.DisabledPlugins, newDisabled)

		return nil, nil
	})
	return err == nil, err
}

func (r *mutationResolver) SetPluginHookOrder(ctx context.Context, input PluginHookOrderInput) ([]*PluginHookOrder, error) {
	if err := r.settings.update(ctx, func(c *config.Config) (func(), error) {
		c.SetPluginHookOrderForHook(input.Hook, input.PluginIds)
		return nil, nil
	}); err != nil {
		return nil, err
	}

	return r.Query().PluginHookOrder(ctx)
}
