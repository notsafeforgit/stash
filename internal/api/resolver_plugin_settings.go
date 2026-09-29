package api

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/plugin"
)

func (r *queryResolver) PluginSettings(ctx context.Context, pluginID string) (*PluginSettings, error) {
	p := manager.GetInstance().PluginCache.GetPlugin(pluginID)
	if p == nil {
		return nil, fmt.Errorf("plugin %q not found", pluginID)
	}
	if p.APIVersion == 3 {
		return nil, fmt.Errorf("plugin %q uses apiVersion 3; use pluginSettingsV3", pluginID)
	}
	definitions := make([]*plugin.PluginSetting, len(p.Settings))
	for i := range p.Settings {
		definitions[i] = &p.Settings[i]
	}
	return &PluginSettings{Definitions: definitions, Values: p.SettingsValues(config.GetInstance().GetPluginConfiguration(pluginID))}, nil
}

func (r *mutationResolver) UpdatePluginSettings(ctx context.Context, pluginID string, input map[string]interface{}, reset []string) (map[string]interface{}, error) {
	p := manager.GetInstance().PluginCache.GetPlugin(pluginID)
	if p == nil {
		return nil, fmt.Errorf("plugin %q not found", pluginID)
	}
	if p.APIVersion == 3 {
		return nil, fmt.Errorf("plugin %q uses apiVersion 3; use updatePluginSettingsV3", pluginID)
	}
	input = convertMapJSONNumbers(input)
	if err := p.ValidateSettings(input, reset); err != nil {
		return nil, err
	}
	saved, err := config.GetInstance().UpdatePluginConfiguration(pluginID, input, reset)
	if err != nil {
		return nil, err
	}
	return p.SettingsValues(saved), nil
}

func (r *queryResolver) PluginEvaluateJq(ctx context.Context, expression string, input interface{}) ([]interface{}, error) {
	return plugin.EvaluateJQ(ctx, expression, input)
}

func (r *queryResolver) PluginEvaluateMappings(ctx context.Context, mappings map[string]interface{}, input interface{}) (map[string]interface{}, error) {
	return plugin.EvaluateMappings(ctx, mappings, input)
}
