package jq_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/jq"
	"github.com/stretchr/testify/require"
)

func TestReadableTextFilterRetainsPlainValuesAndProjectsMarkup(t *testing.T) {
	for _, tc := range []struct{ input, want any }{
		{nil, nil}, {"", ""}, {" \n\t ", " \n\t "},
		{"  Plain &amp; text <3  ", "  Plain &amp; text <3  "},
		{"<p>One &amp; two</p><div>Three<br>four</div>", "One & two\n\nThree\nfour"},
		{"<ul><li>First</li><li>Second</li></ul>", "First\n\nSecond"},
		{"<p>Read <a href='https://example.invalid'>this</a> <strong>text</strong></p>", "Read this text"},
		{"<p> <br/> </p>", ""},
		{"<DIV>界 &amp; é</DIV>", "界 & é"},
	} {
		result, err := jq.EvaluateJQ(context.Background(), "readable_text", tc.input)
		require.NoError(t, err)
		require.Equal(t, []any{tc.want}, result)
	}
}

func TestReadableTextFilterRejectsNonTextAndExcessiveWork(t *testing.T) {
	for _, input := range []any{false, 0, []any{}, map[string]any{"private": "caption"}} {
		_, err := jq.EvaluateJQ(context.Background(), "readable_text", input)
		require.ErrorContains(t, err, "readable_text requires a string or null")
		require.NotContains(t, err.Error(), "caption")
	}
	_, err := jq.EvaluateJQ(context.Background(), `("x" * 1048577) | readable_text`, nil)
	require.ErrorContains(t, err, "readable_text input exceeds")
}

func TestReadableTextMappingKeepsExplicitClearsAndNeverChangesInput(t *testing.T) {
	input := map[string]any{"details": "<p>First</p><p>Second</p>"}
	direct := map[string]any{"details": ".details | readable_text"}
	result, err := jq.EvaluateMappings(context.Background(), direct, input)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"details": "First\n\nSecond"}, result)
	require.Equal(t, "<p>First</p><p>Second</p>", input["details"])
	for _, empty := range []any{nil, ""} {
		result, err = jq.EvaluateMappings(context.Background(), direct, map[string]any{"details": empty})
		require.NoError(t, err)
		require.Equal(t, map[string]any{"details": empty}, result)
		result, err = jq.EvaluateMappings(context.Background(), map[string]any{"details": `.details | readable_text | select(type == "string" and length > 0)`}, map[string]any{"details": empty})
		require.NoError(t, err)
		require.Empty(t, result)
	}
	result, err = jq.EvaluateMappings(context.Background(), direct, map[string]any{"details": 5})
	require.ErrorContains(t, err, "readable_text")
	require.Nil(t, result)
}
