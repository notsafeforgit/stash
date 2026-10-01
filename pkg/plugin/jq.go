package plugin

import (
	"context"
	"github.com/itchyny/gojq"
	"github.com/stashapp/stash/pkg/jq"
)

const jqMaxBytes = jq.MaxBytes

func compileJQ(expression string) (*gojq.Code, error)          { return jq.Compile(expression) }
func validateJQMappings(mappings map[string]interface{}) error { return jq.ValidateMappings(mappings) }
func EvaluateJQ(ctx context.Context, expression string, input interface{}) ([]interface{}, error) {
	return jq.EvaluateJQ(ctx, expression, input)
}
func EvaluateMappings(ctx context.Context, mappings map[string]interface{}, input interface{}) (map[string]interface{}, error) {
	return jq.EvaluateMappings(ctx, mappings, input)
}
