package jq_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/jq"
	"github.com/stretchr/testify/require"
)

func TestUTCDateConvertsKnownDatesWithoutInventingTimezones(t *testing.T) {
	for _, tc := range []struct {
		input any
		want  any
	}{
		{nil, nil}, {"", nil}, {" \n\t ", nil},
		{"2026-07-05", "2026-07-05"},
		{" 2024-02-29 ", "2024-02-29"},
		{"2026-09-29T22:37:08-07:00", "2026-09-30"},
		{"2026-09-29T00:01:00+02:00", "2026-09-28"},
		{"2026-09-29T23:59:59.999999999-07:00", "2026-09-30"},
		{"2026-09-29T23:59:59.1Z", "2026-09-29"},
		{"2026-09-29T23:59:59.123456Z", "2026-09-29"},
		{"2026-09-29T00:00:00+00:00", "2026-09-29"},
		{"2026-09-29T00:00:00-00:00", "2026-09-29"},
		{"0001-01-01", "0001-01-01"},
		{"9999-12-31", "9999-12-31"},
	} {
		result, err := jq.EvaluateJQ(context.Background(), "utc_date", tc.input)
		require.NoError(t, err, "input: %v", tc.input)
		require.Equal(t, []any{tc.want}, result, "input: %v", tc.input)
	}
}

func TestUTCDateRejectsInvalidOrAmbiguousValues(t *testing.T) {
	for _, input := range []any{
		"2026-02-30", "2026-02-29", "2026-13-01", "2026-00-01", "2026-01-00", "0000-01-01",
		"2026-09-29T22:37:08", "2026-09-29 22:37:08", "2026-09-29 22:37:08Z",
		"2026-9-29", "20260929", "2026-09-29t22:37:08z", "2026-09-29T1:00:00Z",
		"2026-09-29T24:00:00Z", "2026-09-29T23:60:00Z", "2026-09-29T23:59:60Z",
		"2026-09-29T23:59:59,1Z", "2026-09-29T23:59:59.Z", "2026-09-29T23:59:59+0000",
		"2026-09-29T00:00:00+24:00", "2026-09-29T00:00:00+00:60",
		"0001-01-01T00:00:00+01:00", "9999-12-31T23:00:00-02:00",
		strings.Repeat("x", 65), 1790704800, true, []any{}, map[string]any{},
	} {
		_, err := jq.EvaluateJQ(context.Background(), "utc_date", input)
		require.ErrorContains(t, err, "utc_date", "input: %v", input)
	}
}

func TestUTCDateMappingDistinguishesMissingFromInvalid(t *testing.T) {
	input := map[string]any{"date": "2026-09-29T22:37:08.123456789-07:00"}
	mapping := map[string]any{"date": ".date | utc_date | select(. != null)"}
	result, err := jq.EvaluateMappings(context.Background(), mapping, input)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"date": "2026-09-30"}, result)
	require.Equal(t, "2026-09-29T22:37:08.123456789-07:00", input["date"])
	for _, missing := range []any{nil, "", "  "} {
		result, err := jq.EvaluateMappings(context.Background(), mapping, map[string]any{"date": missing})
		require.NoError(t, err)
		require.Empty(t, result)
	}
	_, err = jq.EvaluateMappings(context.Background(), mapping, map[string]any{"date": "2026-02-30"})
	require.ErrorContains(t, err, "utc_date")
	result, err = jq.EvaluateMappings(context.Background(), map[string]any{"date": ".date | utc_date?"}, map[string]any{"date": "2026-02-30"})
	require.NoError(t, err)
	require.Empty(t, result, "only an explicit error-suppression rule may omit invalid dates")
}
