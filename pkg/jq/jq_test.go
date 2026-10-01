package jq_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/jq"
	"github.com/stretchr/testify/require"
)

func TestNativeInputLimitKeepsPluginAndOutputLimits(t *testing.T) {
	input := map[string]interface{}{"payload": strings.Repeat("x", 2<<20), "title": "Retained caption"}
	mappings := map[string]interface{}{"title": ".title"}
	_, err := jq.EvaluateMappings(context.Background(), mappings, input)
	require.ErrorContains(t, err, "jq input exceeds")
	output, err := jq.EvaluateMappingsWithInputLimit(context.Background(), mappings, input, 12<<20)
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{"title": "Retained caption"}, output)
	_, err = jq.EvaluateMappingsWithInputLimit(context.Background(), map[string]interface{}{"title": ".payload"}, input, 12<<20)
	require.ErrorContains(t, err, "jq output exceeds")
	_, err = jq.EvaluateMappingsWithInputLimit(context.Background(), mappings, input, 17<<20)
	require.Error(t, err)
}
