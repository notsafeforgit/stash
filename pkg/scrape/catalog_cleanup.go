package scrape

import "time"

type CatalogCleanupIntent struct {
	PostKey     string
	RequestedAt string
}

// The queue is an outbox written with a historical catalog deletion. Its old
// consumer rechecked post/alias recreation before removing background targets.
// Retaining the event must neither recreate that post nor replay its deletion.
func PrepareCatalogCleanupIntent(values map[string]any, captured time.Time) (*CatalogCleanupIntent, string) {
	if len(values) != 2 {
		return nil, "unknown_cleanup_fields"
	}
	key, ok := enrichmentLegacyString(values["post_key"], false, 4096)
	if !ok {
		return nil, "invalid_cleanup_post_key"
	}
	raw, textOK := values["pruned_at"].(string)
	stamp, timeOK := automationTime(values["pruned_at"])
	if !validEnrichmentBoundary(captured) || !textOK || len(raw) > 64 || !timeOK {
		return nil, "invalid_cleanup_time"
	}
	if stamp.After(captured.Add(time.Minute)) {
		return nil, "cleanup_time_after_snapshot"
	}
	return &CatalogCleanupIntent{PostKey: key, RequestedAt: raw}, ""
}
