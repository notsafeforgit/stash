package scrape_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

var discoveryBoundary = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func discoveryAccountRow(t *testing.T) map[string]any {
	t.Helper()
	body, err := scrape.LegacyCatalogJSON([]any{"reddit", "reddit:handle:juniper", "https://www.reddit.com/user/juniper/submitted/?sort=new"}, 65536)
	require.NoError(t, err)
	return map[string]any{"job_key": scrape.CatalogSnapshotSHA(body), "platform": "reddit", "account_key": "reddit:handle:juniper",
		"profile_url": "https://www.reddit.com/user/juniper/submitted/?sort=new", "status": "retry", "cursor_json": `{"after":"t3_sample"}`,
		"staged_json": nil, "pages": json.Number("67"), "attempts": json.Number("4"), "next_attempt": json.Number("1791028800.00000001"),
		"last_error": "network unavailable", "created_at": "2026-09-29T00:00:00Z", "updated_at": "2026-10-03T01:00:00Z"}
}

func TestAutomationDiscoveryPreservesContinuationAndExactDeadlines(t *testing.T) {
	v := discoveryAccountRow(t)
	r, reason := scrape.PrepareAutomationDiscovery("discovery_accounts", v, discoveryBoundary)
	require.Empty(t, reason)
	require.Equal(t, "held", r.Disposition)
	require.Equal(t, "listing", r.Projection.Phase)
	require.EqualValues(t, 67, *r.Projection.HistoricalPages)
	require.Equal(t, time.UnixMilli(1791028800001).UTC(), *r.Projection.NotBefore, "round deadlines up without a float conversion")
	require.Equal(t, scrape.CatalogSnapshotSHA([]byte(v["cursor_json"].(string))), r.Projection.CursorSHA256)
	for _, status := range []string{"pending", "matching", "complete"} {
		v["status"], v["cursor_json"] = status, "null"
		r, reason := scrape.PrepareAutomationDiscovery("discovery_accounts", v, discoveryBoundary)
		require.Empty(t, reason)
		if status == "complete" {
			require.Equal(t, "historical_listing", r.Disposition)
		} else {
			require.Equal(t, "held", r.Disposition)
		}
	}
	v = discoveryAccountRow(t)
	v["staged_json"] = `{"records":[],"cursor":{"after":"t3_next"},"complete":false,"extractor_version":"legacy"}`
	r, reason = scrape.PrepareAutomationDiscovery("discovery_accounts", v, discoveryBoundary)
	require.Empty(t, reason)
	require.Equal(t, "page", r.Projection.StagedKind)
	v["status"] = "matching"
	v["staged_json"] = `{"target":["c_11111111111111111111111111111111","legacy:post:sample","https://www.reddit.com/comments/abc123"],"payload":{"records":[]}}`
	r, reason = scrape.PrepareAutomationDiscovery("discovery_accounts", v, discoveryBoundary)
	require.Empty(t, reason)
	require.Equal(t, "detail", r.Projection.StagedKind)
	require.Len(t, r.StagedTarget, 3)
}

func TestAutomationDiscoveryRejectsForeignOrMalformedContinuation(t *testing.T) {
	for key, values := range map[string][]any{
		"job_key": {"wrong"}, "platform": {"instagram"}, "pages": {json.Number("-1"), json.Number("1.5"), true},
		"next_attempt": {json.Number("-1"), json.Number("1e999")}, "status": {"running", "done"},
		"profile_url": {"https://other.example/user/juniper/submitted/?sort=new", "https://www.reddit.com/user/juniper/submitted/?sort=new&secret=x"},
		"cursor_json": {`{"cursor":"foreign"}`, `{"after":"a","after":"b"}`, `[]`, ""},
		"staged_json": {`[]`, `{"records":[]}`, `{"records":[],"complete":false,"records":[]}`},
		"updated_at":  {"2027-01-01T00:00:00Z", "2020-01-01T00:00:00Z"},
	} {
		for i, value := range values {
			t.Run(fmt.Sprintf("%s-%d", key, i), func(t *testing.T) {
				v := discoveryAccountRow(t)
				v[key] = value
				r, reason := scrape.PrepareAutomationDiscovery("discovery_accounts", v, discoveryBoundary)
				require.Nil(t, r)
				require.NotEmpty(t, reason)
			})
		}
	}
}

func TestAutomationDiscoveryCandidatesRemainUnconfirmed(t *testing.T) {
	v := map[string]any{"catalog_id": "c_11111111111111111111111111111111", "post_key": "legacy:post:sample", "job_key": nil,
		"evidence_json": `{"titles":["An exact original title"],"texts":[],"dates":["2025-01-01"],"paths":["sample.jpg"],"urls":[],"platform":"reddit","account_key":null,"candidate_url":"https://www.reddit.com/comments/abc123","strict_filename_id":false}`, "status": "lookup"}
	r, reason := scrape.PrepareAutomationDiscovery("discovery_targets", v, discoveryBoundary)
	require.Empty(t, reason)
	require.Equal(t, "lookup", r.Disposition)
	require.Equal(t, "https://www.reddit.com/comments/abc123", r.Projection.CandidateURL)
	for _, status := range []string{"done", "matched", "coalesced", "already_native", "ambiguous", "no_known_profile", "gone", "unsupported", "identity_conflict", "unmatched"} {
		v["status"], v["evidence_json"] = status, `{}`
		r, reason := scrape.PrepareAutomationDiscovery("discovery_targets", v, discoveryBoundary)
		require.Empty(t, reason)
		require.NotNil(t, r)
	}
	v = map[string]any{"catalog_id": "c_11111111111111111111111111111111", "post_key": "legacy:post:sample", "url": "https://www.reddit.com/comments/abc123", "basis": "title-needs-verification", "payload_json": `{"records":[],"extractor_version":"legacy"}`}
	r, reason = scrape.PrepareAutomationDiscovery("discovery_candidates", v, discoveryBoundary)
	require.Empty(t, reason)
	require.Equal(t, "candidate", r.Disposition)
	require.Equal(t, "title-needs-verification", r.Projection.CandidateBasis)
	v["basis"] = "fuzzy-caption"
	_, reason = scrape.PrepareAutomationDiscovery("discovery_candidates", v, discoveryBoundary)
	require.Equal(t, "unsupported_legacy_discovery_candidate_basis", reason)
}

func TestAutomationDiscoveryMaintenanceNeverBecomesLiveProgress(t *testing.T) {
	rows := map[string]string{
		"inventory-watermark-ns":      "1790936092595039483",
		"translation-seed-v1":         "2026-09-29T09:26:06Z",
		"translation-seed-en-v2":      "2026-09-29T14:16:28Z",
		"metadata-enrichment-seed-v1": `{"posts_examined":9007199254740993}`,
		"metadata-discovery-seed-v1":  `{"accounts":6,"account_scan_targets":569}`,
		"enrichment-excluded-sources": `{"requested_at":"2026-09-29T22:32:08Z","sources":["thisvid"]}`,
		"metadata-pruning-last-run":   `{"at":"2026-10-02T10:27:37Z","applied":true,"counts":{"files":1209},"affected_catalogs":["c_11111111111111111111111111111111"],"ambiguous_assets_retained":0}`,
	}
	for key, value := range rows {
		t.Run(key, func(t *testing.T) {
			r, reason := scrape.PrepareAutomationDiscovery("maintenance", map[string]any{"key": key, "value": value}, discoveryBoundary)
			require.Empty(t, reason)
			require.Equal(t, "maintenance_history", r.Disposition)
			require.Nil(t, r.Projection.NotBefore)
			require.Equal(t, scrape.CatalogSnapshotSHA([]byte(value)), r.Projection.PayloadSHA256)
			if key == "inventory-watermark-ns" {
				require.Equal(t, value, r.Projection.WatermarkNS)
			}
		})
	}
	for _, v := range []map[string]any{{"key": "future-key", "value": "{}"}, {"key": "inventory-watermark-ns", "value": "1790936092595039483.0"}, {"key": "metadata-enrichment-seed-v1", "value": `{"count":-1}`}} {
		_, reason := scrape.PrepareAutomationDiscovery("maintenance", v, discoveryBoundary)
		require.NotEmpty(t, reason)
	}
	// Evidence decoding keeps exact integers and rejects duplicate keys.
	_, err := archive.DecodeJSONObject([]byte(`{"n":1,"n":2}`), 1024)
	require.Error(t, err)
}
