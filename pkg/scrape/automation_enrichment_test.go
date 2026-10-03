package scrape_test

import (
	"encoding/json"
	"maps"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

var enrichmentFrozenAt = time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)

func legacyEnrichmentJob() map[string]any {
	return map[string]any{"catalog_id": "c_11111111111111111111111111111111", "post_key": "onlyfans:post:123",
		"version": json.Number("1"), "platform": "onlyfans", "account_key": "onlyfans:id:42",
		"url": "https://coomer.st/onlyfans/user/42/post/123", "status": "retry", "priority": json.Number("25"),
		"attempts": json.Number("9"), "next_attempt": json.Number("1791032400.000001"), "last_error": "Source failed",
		"staged_json": nil, "created_at": "2026-09-29T00:00:00Z", "updated_at": "2026-09-30T00:00:00Z"}
}

func TestAutomationEnrichmentPreservesRetryAndActualMirrorService(t *testing.T) {
	values := legacyEnrichmentJob()
	before, err := json.Marshal(values)
	require.NoError(t, err)
	job, reason := scrape.PrepareAutomationEnrichmentJob(values, enrichmentFrozenAt)
	require.Empty(t, reason)
	require.Equal(t, "held", job.Disposition)
	require.Equal(t, "mirror:coomer", job.ServiceScope)
	require.Equal(t, "onlyfans", job.Platform, "source account namespace is not the contacted service")
	require.EqualValues(t, 9, job.Attempts)
	require.Equal(t, 25, job.Priority)
	require.Equal(t, "2026-09-29T00:00:00Z", job.CreatedAt.Format(time.RFC3339))
	require.Equal(t, "2026-09-30T00:00:00Z", job.UpdatedAt.Format(time.RFC3339))
	require.EqualValues(t, 1791032400001, job.NotBefore.UnixMilli())
	for token, millis := range map[string]int64{"1791032400.0000001": 1791032400001, "1.7910324000000001e9": 1791032400001, "1791032400.001": 1791032400001} {
		changed := maps.Clone(values)
		changed["next_attempt"] = json.Number(token)
		projected, reason := scrape.PrepareAutomationEnrichmentJob(changed, enrichmentFrozenAt)
		require.Empty(t, reason)
		require.Equal(t, millis, projected.NotBefore.UnixMilli(), token)
	}
	values["platform"], values["url"] = "patreon", "https://kemono.cr/patreon/user/42/post/123"
	job, reason = scrape.PrepareAutomationEnrichmentJob(values, enrichmentFrozenAt)
	require.Empty(t, reason)
	require.Equal(t, "mirror:kemono", job.ServiceScope)
	values["platform"], values["url"] = "onlyfans", "https://coomer.st/onlyfans/user/42/post/123"
	after, err := json.Marshal(values)
	require.NoError(t, err)
	require.Equal(t, before, after)
	values["next_attempt"] = json.Number("0")
	job, reason = scrape.PrepareAutomationEnrichmentJob(values, enrichmentFrozenAt)
	require.Empty(t, reason)
	require.Equal(t, enrichmentFrozenAt, job.NotBefore)
}

func TestAutomationEnrichmentLabelsRequireTheirOriginalProof(t *testing.T) {
	for _, test := range []struct{ status, disposition, proof string }{
		{"pending", "held", ""}, {"retry", "held", ""},
		{"done", "historical_completion", "catalog_receipt"},
		{"already_native", "source_present", "gallery_dl_capture"},
		{"coalesced", "coalesced", "post_alias"}, {"excluded_source", "excluded", ""},
		{"gone", "review", ""}, {"no_post_url", "review", ""}, {"unsupported", "review", ""},
		{"identity_conflict", "review", ""}, {"unmatched", "review", ""},
	} {
		t.Run(test.status, func(t *testing.T) {
			values := legacyEnrichmentJob()
			values["status"] = test.status
			if test.status == "excluded_source" || test.status == "no_post_url" {
				values["url"] = nil
			}
			job, reason := scrape.PrepareAutomationEnrichmentJob(values, enrichmentFrozenAt)
			require.Empty(t, reason)
			require.Equal(t, test.disposition, job.Disposition)
			require.Equal(t, test.proof, job.EvidenceRequired)
		})
	}
	values := legacyEnrichmentJob()
	values["status"], values["platform"], values["url"] = "excluded_source", "thisvidmember", "https://thisvid.com/videos/example/"
	job, reason := scrape.PrepareAutomationEnrichmentJob(values, enrichmentFrozenAt)
	require.Empty(t, reason)
	require.Equal(t, "excluded", job.Disposition)
}

func TestAutomationEnrichmentRetainsStagedBytesEvenUnderCompletedLabel(t *testing.T) {
	values := legacyEnrichmentJob()
	staged := " {\"records\": [{\"id\": 9007199254740993}], \"pending_children\": []} "
	values["staged_json"], values["status"] = staged, "done"
	job, reason := scrape.PrepareAutomationEnrichmentJob(values, enrichmentFrozenAt)
	require.Empty(t, reason)
	require.Equal(t, "staged_review", job.Disposition)
	require.Equal(t, "legacy_checkpoint_conversion", job.EvidenceRequired)
	require.Equal(t, scrape.CatalogSnapshotSHA([]byte(staged)), job.StagedSHA256)
	require.Equal(t, staged, values["staged_json"])
}

func TestAutomationEnrichmentRejectsChangedVersionsAndInvalidScheduling(t *testing.T) {
	for _, test := range []struct {
		key, reason string
		value       any
	}{
		{"catalog_id", "invalid_legacy_catalog", "../outside"},
		{"post_key", "invalid_legacy_post_key", ""},
		{"version", "unsupported_legacy_enrichment_version", json.Number("2")},
		{"version", "unsupported_legacy_enrichment_version", "1"},
		{"platform", "invalid_legacy_platform", nil},
		{"account_key", "invalid_legacy_account_key", false},
		{"url", "unsupported_legacy_enrichment_url", nil},
		{"url", "unsupported_legacy_enrichment_url", "https://coomer.st/onlyfans/user/42"},
		{"url", "invalid_legacy_url", 1},
		{"priority", "invalid_legacy_schedule", json.Number("101")},
		{"attempts", "invalid_legacy_schedule", json.Number("-1")},
		{"next_attempt", "invalid_legacy_schedule", json.Number("1e99")},
		{"next_attempt", "invalid_legacy_schedule", json.Number("1e-999999999")},
		{"next_attempt", "invalid_legacy_schedule", json.Number("1/2")},
		{"next_attempt", "invalid_legacy_schedule", json.Number("-1")},
		{"next_attempt", "invalid_legacy_schedule", "later"},
		{"updated_at", "invalid_legacy_schedule", "2025-01-01T00:00:00Z"},
		{"updated_at", "invalid_legacy_schedule", "2026-10-03T00:00:00Z"},
		{"status", "unsupported_legacy_enrichment_status", "running"},
		{"last_error", "invalid_legacy_error", []string{"error"}},
		{"staged_json", "invalid_legacy_enrichment_staging", "[]"},
		{"staged_json", "invalid_legacy_enrichment_staging", `{"records":[],"records":[]}`},
	} {
		t.Run(test.key+"/"+test.reason, func(t *testing.T) {
			values := legacyEnrichmentJob()
			values[test.key] = test.value
			job, reason := scrape.PrepareAutomationEnrichmentJob(values, enrichmentFrozenAt)
			require.Nil(t, job)
			require.Equal(t, test.reason, reason)
		})
	}
	job, reason := scrape.PrepareAutomationEnrichmentJob(legacyEnrichmentJob(), time.Time{})
	require.Nil(t, job)
	require.Equal(t, "invalid_legacy_schedule", reason)
}

func TestEnrichmentCooldownsPreserveAccountAndPlatformBoundaries(t *testing.T) {
	job, reason := scrape.PrepareAutomationEnrichmentJob(legacyEnrichmentJob(), enrichmentFrozenAt)
	require.Empty(t, reason)
	var cooldowns []scrape.AutomationEnrichmentCooldown
	for index, scope := range []string{"platform:onlyfans", "account:onlyfans:id:42", "account:onlyfans:id:other", "platform:coomer"} {
		cooldown, reason := scrape.PrepareAutomationEnrichmentCooldown(map[string]any{"scope": scope,
			"until_time": json.Number([]string{"1791042400.000001", "1791052400.000001", "1791062400", "1791072400"}[index]),
			"reason":     "access_denied"}, enrichmentFrozenAt)
		require.Empty(t, reason)
		cooldowns = append(cooldowns, *cooldown)
	}
	require.Equal(t, "platform", cooldowns[0].Kind)
	require.Equal(t, "onlyfans", cooldowns[0].Value, "do not reinterpret a legacy label as a native service")
	require.EqualValues(t, 1791052400001, scrape.EnrichmentNotBefore(*job, cooldowns, time.Time{}).UnixMilli())
	native := time.Unix(1791092400, 0)
	require.Equal(t, native, scrape.EnrichmentNotBefore(*job, cooldowns, native))
	job.AccountKey = nil
	require.EqualValues(t, 1791042400001, scrape.EnrichmentNotBefore(*job, cooldowns, time.Time{}).UnixMilli())
	job.Platform = "twitter"
	require.Equal(t, job.NotBefore, scrape.EnrichmentNotBefore(*job, cooldowns, time.Time{}))
	for _, scope := range []string{"onlyfans", "platform:", "service:onlyfans", "account:"} {
		_, reason := scrape.PrepareAutomationEnrichmentCooldown(map[string]any{"scope": scope, "reason": "timeout", "until_time": json.Number("1")}, enrichmentFrozenAt)
		require.Equal(t, "invalid_legacy_cooldown_scope", reason)
	}
	_, reason = scrape.PrepareAutomationEnrichmentCooldown(map[string]any{"scope": "platform:twitter", "reason": "unknown_policy", "until_time": json.Number("1791042400")}, enrichmentFrozenAt)
	require.Equal(t, "unsupported_legacy_cooldown_reason", reason)
}

func TestEnrichmentSeedAndSourceProgressAreHistoricalNotExecutionProof(t *testing.T) {
	values := map[string]any{"catalog_id": "c_11111111111111111111111111111111", "last_post_key": "reddit:post:abc",
		"complete": json.Number("1"), "counts_json": `{"posts_examined":9,"pending":5}`}
	seed, reason := scrape.PrepareAutomationEnrichmentSeed(values)
	require.Empty(t, reason)
	require.True(t, seed.Complete)
	require.Equal(t, "reddit:post:abc", seed.LastPostKey)
	require.EqualValues(t, 5, seed.Counts["pending"], "completed enumeration can still leave pending jobs")
	for key, value := range map[string]any{"complete": true, "catalog_id": "wrong", "counts_json": `{"pending":-1}`, "last_post_key": nil} {
		changed := maps.Clone(values)
		changed[key] = value
		_, reason := scrape.PrepareAutomationEnrichmentSeed(changed)
		require.NotEmpty(t, reason)
	}
	progress, reason := scrape.PrepareAutomationEnrichmentSourceProgress(map[string]any{"platform": "onlyfans", "last_attempt": json.Number("1790842400.125")}, enrichmentFrozenAt)
	require.Empty(t, reason)
	require.EqualValues(t, 1790842400125, progress.LastAttempt.UnixMilli())
	for _, value := range []any{json.Number("0"), json.Number("-1"), json.Number("1e99"), "1790842400"} {
		_, reason := scrape.PrepareAutomationEnrichmentSourceProgress(map[string]any{"platform": "onlyfans", "last_attempt": value}, enrichmentFrozenAt)
		require.Equal(t, "invalid_legacy_enrichment_source_progress", reason)
	}
}

func TestCatalogEnrichmentReceiptsKeepHistoricalTimeAndUnresolvedChildren(t *testing.T) {
	values := map[string]any{"post_key": "reddit:post:abc", "version": json.Number("1"),
		"completed_at": "2026-10-01T01:02:03.123456+02:00", "details_json": `{"attachment_links_enriched":3,"unresolved_children":2}`}
	receipt, reason := scrape.PrepareCatalogEnrichmentReceipt(values, enrichmentFrozenAt)
	require.Empty(t, reason)
	require.Equal(t, "2026-09-30T23:02:03.123456Z", receipt.CompletedAt.UTC().Format(time.RFC3339Nano))
	require.EqualValues(t, 3, receipt.AttachmentLinksEnriched)
	require.EqualValues(t, 2, receipt.UnresolvedChildren, "historical completion does not imply exhaustive child coverage")
	for key, value := range map[string]any{"version": json.Number("2"), "completed_at": "2026-10-04T00:00:00Z", "details_json": `{"attachment_links_enriched":3}`} {
		changed := maps.Clone(values)
		changed[key] = value
		_, reason := scrape.PrepareCatalogEnrichmentReceipt(changed, enrichmentFrozenAt)
		require.NotEmpty(t, reason)
	}
}
