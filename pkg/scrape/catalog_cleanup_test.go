package scrape

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPrepareCatalogCleanupRetainsOriginalEvidence(t *testing.T) {
	captured := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	values := map[string]any{"post_key": "reddit:post:removed", "pruned_at": "2026-09-30T16:59:00.123456789-07:00"}
	before := maps.Clone(values)
	intent, reason := PrepareCatalogCleanupIntent(values, captured)
	require.Empty(t, reason)
	require.Equal(t, &CatalogCleanupIntent{PostKey: values["post_key"].(string), RequestedAt: values["pruned_at"].(string)}, intent)
	require.Equal(t, before, values)
	for _, test := range []struct {
		name, field, reason string
		value               any
	}{
		{"extra", "extra", "unknown_cleanup_fields", true},
		{"empty_key", "post_key", "invalid_cleanup_post_key", ""},
		{"long_key", "post_key", "invalid_cleanup_post_key", strings.Repeat("a", 4097)},
		{"null_key", "post_key", "invalid_cleanup_post_key", nil},
		{"invalid_time", "pruned_at", "invalid_cleanup_time", "today"},
		{"null_time", "pruned_at", "invalid_cleanup_time", nil},
		{"local_time", "pruned_at", "invalid_cleanup_time", "2026-09-30T16:59:00"},
		{"future_time", "pruned_at", "cleanup_time_after_snapshot", "2026-10-01T00:01:01Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := maps.Clone(values)
			changed[test.field] = test.value
			intent, reason := PrepareCatalogCleanupIntent(changed, captured)
			require.Nil(t, intent)
			require.Equal(t, test.reason, reason)
		})
	}
	intent, reason = PrepareCatalogCleanupIntent(values, time.Time{})
	require.Nil(t, intent)
	require.Equal(t, "invalid_cleanup_time", reason)
}
