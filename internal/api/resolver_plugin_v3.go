package api

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/plugin"
)

// These models deliberately do not embed the legacy GraphQL plugin types.
type PluginV3 struct {
	ID          string
	APIVersion  int
	Name        string
	Description *string
	URL         *string
	Version     *string
	Enabled     bool
	Tasks       []*PluginTaskV3
	Hooks       []*PluginHookV3
	Settings    []plugin.PluginSettingV3
	Requires    []string
	Paths       *PluginPathsV3
}

type PluginPathsV3 struct{ Entry *string }

type PluginTaskV3 struct {
	Name        string
	Description *string
	Plugin      *PluginV3
}

type PluginHookV3 struct {
	Name        string
	Description *string
	Hooks       []string
	Plugin      *PluginV3
}

type PluginSettingsV3 struct {
	Definitions []plugin.PluginSettingV3
	Values      map[string]interface{}
}

func pluginV3(ctx context.Context, p *plugin.Plugin) *PluginV3 {
	baseURL, _ := ctx.Value(BaseURLCtxKey).(string)
	ret := &PluginV3{
		ID: p.ID, APIVersion: p.APIVersion, Name: p.Name, Description: p.Description,
		URL: p.URL, Version: p.Version, Enabled: p.Enabled, Settings: p.SettingsV3,
		Requires: p.UI.Requires, Paths: &PluginPathsV3{Entry: (pluginURLBuilder{BaseURL: baseURL, Plugin: p}).entry()},
	}
	for _, task := range p.Tasks {
		ret.Tasks = append(ret.Tasks, &PluginTaskV3{Name: task.Name, Description: task.Description, Plugin: ret})
	}
	for _, hook := range p.Hooks {
		ret.Hooks = append(ret.Hooks, &PluginHookV3{Name: hook.Name, Description: hook.Description, Hooks: hook.Hooks, Plugin: ret})
	}
	return ret
}

func (r *queryResolver) PluginsV3(ctx context.Context) ([]*PluginV3, error) {
	plugins := manager.GetInstance().PluginCache.ListPlugins()
	ret := make([]*PluginV3, 0, len(plugins))
	for _, p := range plugins {
		ret = append(ret, pluginV3(ctx, p))
	}
	return ret, nil
}

func (r *queryResolver) PluginTasksV3(ctx context.Context) ([]*PluginTaskV3, error) {
	plugins, err := r.PluginsV3(ctx)
	if err != nil {
		return nil, err
	}
	var ret []*PluginTaskV3
	for _, p := range plugins {
		if p.Enabled {
			ret = append(ret, p.Tasks...)
		}
	}
	return ret, nil
}

func (r *queryResolver) PluginSettingsV3(ctx context.Context, pluginID string) (*PluginSettingsV3, error) {
	p := manager.GetInstance().PluginCache.GetPlugin(pluginID)
	if p == nil {
		return nil, fmt.Errorf("plugin %q not found", pluginID)
	}
	return &PluginSettingsV3{Definitions: p.SettingsV3, Values: p.SettingsValuesV3(config.GetInstance().GetPluginConfiguration(pluginID))}, nil
}

func (r *mutationResolver) UpdatePluginSettingsV3(ctx context.Context, pluginID string, input map[string]interface{}, reset []string) (map[string]interface{}, error) {
	p := manager.GetInstance().PluginCache.GetPlugin(pluginID)
	if p == nil {
		return nil, fmt.Errorf("plugin %q not found", pluginID)
	}
	input = convertMapJSONNumbers(input)
	if err := p.ValidateSettingsV3(input, reset); err != nil {
		return nil, err
	}
	saved, err := config.GetInstance().UpdatePluginConfiguration(pluginID, input, reset)
	if err != nil {
		return nil, err
	}
	return p.SettingsValuesV3(saved), nil
}
