package api

import (
	"context"

	"github.com/stashapp/stash/pkg/plugin"
)

func (r *queryResolver) PluginEvaluateJq(ctx context.Context, expression string, input interface{}) ([]interface{}, error) {
	return plugin.EvaluateJQ(ctx, expression, input)
}

func (r *queryResolver) PluginEvaluateMappings(ctx context.Context, mappings map[string]interface{}, input interface{}) (map[string]interface{}, error) {
	return plugin.EvaluateMappings(ctx, mappings, input)
}
