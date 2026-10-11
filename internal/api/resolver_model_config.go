package api

import (
	"context"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
)

func (r *configGeneralResultResolver) SourceTrackingActive(ctx context.Context, obj *ConfigGeneralResult) (bool, error) {
	return r.sourceTrackingActive, nil
}

func (r *configGeneralResultResolver) Stashes(ctx context.Context, obj *ConfigGeneralResult) ([]*config.StashConfig, error) {
	mgr := manager.GetInstance()
	if mgr.Database.Ready() != nil {
		return config.GetInstance().GetStashPaths(), nil // setup/migration input only
	}
	return manager.LibraryPaths(ctx, r.repository)
}

func (r *configResultResolver) Plugins(ctx context.Context, obj *ConfigResult, include []string) (map[string]map[string]interface{}, error) {
	if len(include) == 0 {
		ret := config.GetInstance().GetAllPluginConfiguration()
		return ret, nil
	}

	ret := make(map[string]map[string]interface{})

	for _, plugin := range include {
		c := config.GetInstance().GetPluginConfiguration(plugin)
		if len(c) > 0 {
			ret[plugin] = c
		}
	}

	return ret, nil
}
