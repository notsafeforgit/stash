package scrape_test

import (
	"encoding/json"
	"maps"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func legacyTranslationJob(t *testing.T, original string, result any) map[string]any {
	t.Helper()
	identity, err := scrape.LegacyCatalogJSON([]any{original, "en"}, 65536)
	require.NoError(t, err)
	var cached any
	if result != nil {
		body, err := json.Marshal(result)
		require.NoError(t, err)
		cached = string(body)
	}
	return map[string]any{"job_key": scrape.CatalogSnapshotSHA(identity), "original_text": original, "target_language": "en",
		"priority": json.Number("25"), "source_hint": nil, "status": "pending", "result_json": cached,
		"attempts": json.Number("10"), "next_attempt": json.Number("1791032400.000001"), "last_error": "Original retry status",
		"created_at": "2026-09-30T01:02:03Z", "updated_at": "2026-10-01T01:02:03Z"}
}

func TestAutomationTranslationPreservesOriginalEnglishAndUnknownProviderTime(t *testing.T) {
	original := "  <b>Keep exact text 🌿</b>\n\u2028\\u2028 "
	result := map[string]any{"status": "english", "source_language": "en", "translated_text": "Old provider rewrite", "provider": "translate-shell/bing"}
	values := legacyTranslationJob(t, original, result)
	values["status"] = "done"
	before, err := json.Marshal(values)
	require.NoError(t, err)
	captured := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	prepared, reason := scrape.PrepareAutomationTranslationJob(values, captured)
	require.Empty(t, reason)
	require.Equal(t, "english_original", prepared.Disposition)
	require.Equal(t, original, *prepared.Cache.TranslatedText)
	require.Equal(t, "unchanged", prepared.Cache.Status)
	require.Empty(t, prepared.Cache.CapturedAt, "job update time is not provider capture time")
	require.EqualValues(t, 10, prepared.Attempts)
	require.Equal(t, 25, prepared.Priority)
	require.EqualValues(t, 1791032400001, prepared.NotBefore.UnixMilli(), "fractional retry cannot run earlier")
	request, err := archive.PrepareTranslationRequest(prepared.Request)
	require.NoError(t, err)
	_, _, err = archive.PrepareTranslationCache(request, *prepared.Cache)
	require.NoError(t, err)
	after, err := json.Marshal(values)
	require.NoError(t, err)
	require.Equal(t, before, after, "original cache and all source values remain unchanged")
	values["next_attempt"] = json.Number("0")
	prepared, reason = scrape.PrepareAutomationTranslationJob(values, captured)
	require.Empty(t, reason)
	require.Equal(t, captured, prepared.NotBefore)
}

func TestAutomationTranslationKeepsUnusableInputForReview(t *testing.T) {
	captured := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	values := legacyTranslationJob(t, "Original", nil)
	for _, test := range []struct {
		key    string
		value  any
		reason string
	}{
		{"job_key", "bad", "invalid_job_key"},
		{"job_key", scrape.CatalogSnapshotSHA(nil), "legacy_job_hash_mismatch"},
		{"target_language", "ja", "unsupported_legacy_target_language"},
		{"priority", json.Number("101"), "invalid_legacy_schedule"},
		{"attempts", json.Number("-1"), "invalid_legacy_schedule"},
		{"next_attempt", json.Number("1e99"), "invalid_legacy_schedule"},
		{"next_attempt", json.Number("-1"), "invalid_legacy_schedule"},
		{"next_attempt", "later", "invalid_legacy_schedule"},
		{"updated_at", "2026-10-03T00:00:00Z", "invalid_legacy_schedule"},
		{"updated_at", "2025-01-01T00:00:00Z", "invalid_legacy_schedule"},
		{"source_hint", true, "invalid_legacy_job_values"},
		{"status", "unknown", "unsupported_legacy_job_status"},
		{"status", "done", "completed_cache_missing"},
		{"result_json", "{broken", "invalid_legacy_cache"},
		{"result_json", `{"status":"english","status":"translated","source_language":"en","translated_text":"Original","provider":"translate-shell/bing"}`, "invalid_legacy_cache"},
		{"result_json", `{"status":"english","source_language":"ja","translated_text":"Original","provider":"translate-shell/bing"}`, "unproven_english_cache"},
		{"result_json", `{"status":"translated","source_language":"ja","translated_text":"Result","provider":"other"}`, "unsupported_legacy_provider"},
		{"result_json", `{"status":"no-text","source_language":null,"translated_text":"invented","provider":null}`, "invalid_legacy_cache"},
	} {
		t.Run(test.key+"/"+test.reason, func(t *testing.T) {
			changed := maps.Clone(values)
			changed[test.key] = test.value
			prepared, reason := scrape.PrepareAutomationTranslationJob(changed, captured)
			require.Nil(t, prepared)
			require.Equal(t, test.reason, reason)
		})
	}
	values["status"] = "preserved"
	prepared, reason := scrape.PrepareAutomationTranslationJob(values, captured)
	require.Empty(t, reason)
	require.Nil(t, prepared.Cache, "a preserved label does not manufacture a provider outcome")
}

func TestAutomationTranslationTargetUsesOnlyKnownFieldsAndExactKeys(t *testing.T) {
	values := map[string]any{"job_key": scrape.CatalogSnapshotSHA(nil), "catalog_id": "c_11111111111111111111111111111111", "post_key": "reddit:post:example", "field": "caption", "applied": json.Number("1")}
	ret, reason := scrape.PrepareAutomationTranslationTarget(values)
	require.Empty(t, reason)
	require.True(t, ret.Applied)
	for key, value := range map[string]any{"field": "details", "applied": json.Number("2"), "catalog_id": "../outside", "job_key": "bad", "post_key": nil} {
		changed := maps.Clone(values)
		changed[key] = value
		_, reason := scrape.PrepareAutomationTranslationTarget(changed)
		require.Equal(t, "invalid_target_values", reason)
	}
}
