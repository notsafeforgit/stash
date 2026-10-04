package scrape

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const AutomationDiscoveryPolicy = "automation-discovery-v1"

type AutomationDiscoveryPrepared struct {
	Projection                                               models.AutomationDiscoveryProjection
	CatalogID, PostKey, JobKey, AccountKey, Platform, Status string
	Disposition                                              string
	StagedTarget                                             []string
}

var discoveryTwitterProfile = regexp.MustCompile(`^/(?:id:[0-9]+|[A-Za-z0-9_]{1,30})/timeline$`)
var discoveryRedditProfile = regexp.MustCompile(`^/user/[A-Za-z0-9_-]{1,40}/submitted/$`)

func PrepareAutomationDiscovery(table string, values map[string]any, captured time.Time) (*AutomationDiscoveryPrepared, string) {
	if !validEnrichmentBoundary(captured) {
		return nil, "invalid_legacy_discovery_boundary"
	}
	switch table {
	case "discovery_accounts":
		return prepareDiscoveryAccount(values, captured)
	case "discovery_targets":
		return prepareDiscoveryTarget(values)
	case "discovery_candidates":
		return prepareDiscoveryCandidate(values)
	case "maintenance":
		return prepareDiscoveryMaintenance(values, captured)
	default:
		return nil, "unsupported_legacy_discovery_family"
	}
}

func discoveryObject(value any, maximum int) (map[string]any, string, bool) {
	raw, ok := value.(string)
	if !ok || raw == "" || len(raw) > maximum {
		return nil, "", false
	}
	object, err := archive.DecodeJSONObject([]byte(raw), maximum)
	return object, CatalogSnapshotSHA([]byte(raw)), err == nil
}

func prepareDiscoveryAccount(v map[string]any, captured time.Time) (*AutomationDiscoveryPrepared, string) {
	r := &AutomationDiscoveryPrepared{}
	var ok bool
	r.Platform, ok = v["platform"].(string)
	if !ok || (r.Platform != "twitter" && r.Platform != "reddit") {
		return nil, "unsupported_legacy_discovery_platform"
	}
	r.AccountKey, ok = enrichmentLegacyString(v["account_key"], false, 8192)
	if !ok {
		return nil, "invalid_legacy_discovery_account"
	}
	p := &r.Projection
	p.ProfileURL, ok = enrichmentLegacyString(v["profile_url"], false, 8192)
	if !ok {
		return nil, "invalid_legacy_discovery_profile"
	}
	u, err := url.Parse(p.ProfileURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawPath != "" ||
		(r.Platform == "twitter" && (u.Host != "x.com" || !discoveryTwitterProfile.MatchString(u.Path) || u.RawQuery != "")) ||
		(r.Platform == "reddit" && (u.Host != "www.reddit.com" || !discoveryRedditProfile.MatchString(u.Path) || u.RawQuery != "sort=new")) {
		return nil, "invalid_legacy_discovery_profile"
	}
	p.ServiceScope, _ = SourceScopeV1(p.ProfileURL)
	r.JobKey, ok = v["job_key"].(string)
	identity, err := LegacyCatalogJSON([]any{r.Platform, r.AccountKey, p.ProfileURL}, 32768)
	if !ok || err != nil || CatalogSnapshotSHA(identity) != r.JobKey {
		return nil, "invalid_legacy_discovery_job_key"
	}
	pages, pagesOK := automationInteger(v["pages"])
	attempts, attemptsOK := automationInteger(v["attempts"])
	deadline, deadlineOK := enrichmentLegacyDeadline(v["next_attempt"], captured)
	created, createdOK := automationTime(v["created_at"])
	updated, updatedOK := automationTime(v["updated_at"])
	if !pagesOK || pages < 0 || !attemptsOK || attempts < 0 || !deadlineOK || !createdOK || !updatedOK ||
		updated.Before(created) || updated.After(captured.Add(time.Minute)) {
		return nil, "invalid_legacy_discovery_schedule"
	}
	p.HistoricalPages, p.HistoricalAttempts, p.NotBefore = &pages, &attempts, &deadline
	if v["last_error"] != nil {
		if _, ok := enrichmentLegacyString(v["last_error"], true, CatalogChunkLimit); !ok {
			return nil, "invalid_legacy_discovery_error"
		}
	}
	r.Status, _ = v["status"].(string)
	switch r.Status {
	case "pending", "retry":
		p.Phase, r.Disposition = "listing", "held"
	case "matching":
		p.Phase, r.Disposition = "matching", "held"
	case "complete":
		p.Phase, r.Disposition = "complete", "historical_listing"
	default:
		return nil, "unsupported_legacy_discovery_status"
	}
	if v["cursor_json"] != nil {
		raw, ok := v["cursor_json"].(string)
		if !ok || len(raw) > 65536 {
			return nil, "invalid_legacy_discovery_cursor"
		}
		if strings.TrimSpace(raw) != "null" {
			cursor, _, valid := discoveryObject(raw, 65536)
			key := "cursor"
			if r.Platform == "reddit" {
				key = "after"
			}
			if !valid || len(cursor) != 1 {
				return nil, "invalid_legacy_discovery_cursor"
			}
			if _, valid := enrichmentLegacyString(cursor[key], false, 65536); !valid {
				return nil, "invalid_legacy_discovery_cursor"
			}
		}
		p.CursorSHA256 = CatalogSnapshotSHA([]byte(raw))
	}
	if v["staged_json"] != nil {
		staged, sha, valid := discoveryObject(v["staged_json"], CatalogChunkLimit)
		if !valid || p.Phase == "complete" {
			return nil, "invalid_legacy_discovery_staging"
		}
		p.StagedSHA256, p.StagedKind = sha, "page"
		if p.Phase == "matching" {
			target, ok := staged["target"].([]any)
			if !ok || len(target) != 3 {
				return nil, "invalid_legacy_discovery_staging"
			}
			for _, item := range target {
				text, ok := enrichmentLegacyString(item, false, 16384)
				if !ok {
					return nil, "invalid_legacy_discovery_staging"
				}
				r.StagedTarget = append(r.StagedTarget, text)
			}
			if !catalogSnapshotID.MatchString(r.StagedTarget[0]) || !archive.EnrichmentPostURL(r.StagedTarget[2]) {
				return nil, "invalid_legacy_discovery_staging"
			}
			p.CandidateURL = r.StagedTarget[2]
			if _, ok := staged["payload"].(map[string]any); !ok {
				return nil, "invalid_legacy_discovery_staging"
			}
			p.StagedKind = "detail"
		} else {
			if _, ok := staged["records"].([]any); !ok {
				return nil, "invalid_legacy_discovery_staging"
			}
			if _, ok := staged["complete"].(bool); !ok {
				return nil, "invalid_legacy_discovery_staging"
			}
		}
	}
	return r, ""
}

func discoveryPost(v map[string]any) (*AutomationDiscoveryPrepared, string) {
	r := &AutomationDiscoveryPrepared{}
	var ok bool
	r.CatalogID, ok = v["catalog_id"].(string)
	if !ok || !catalogSnapshotID.MatchString(r.CatalogID) {
		return nil, "invalid_legacy_catalog"
	}
	r.PostKey, ok = enrichmentLegacyString(v["post_key"], false, 16384)
	if !ok {
		return nil, "invalid_legacy_post_key"
	}
	return r, ""
}

func discoveryStringList(value any, maximum int) bool {
	items, ok := value.([]any)
	if !ok || len(items) > maximum {
		return false
	}
	for _, item := range items {
		if _, ok := enrichmentLegacyString(item, true, CatalogChunkLimit); !ok {
			return false
		}
	}
	return true
}

func prepareDiscoveryTarget(v map[string]any) (*AutomationDiscoveryPrepared, string) {
	r, reason := discoveryPost(v)
	if reason != "" {
		return nil, reason
	}
	if v["job_key"] != nil {
		var ok bool
		r.JobKey, ok = v["job_key"].(string)
		if !ok || !archive.ValidSHA256(r.JobKey) {
			return nil, "invalid_legacy_discovery_job_key"
		}
	}
	evidence, sha, ok := discoveryObject(v["evidence_json"], CatalogChunkLimit)
	if !ok {
		return nil, "invalid_legacy_discovery_evidence"
	}
	r.Projection.EvidenceSHA256 = sha
	r.Status, _ = v["status"].(string)
	switch r.Status {
	case "pending", "lookup":
		if (r.Status == "pending") != (r.JobKey != "") {
			return nil, "invalid_legacy_discovery_target_binding"
		}
		for _, key := range []string{"titles", "texts", "dates", "paths", "urls"} {
			if !discoveryStringList(evidence[key], 65536) {
				return nil, "invalid_legacy_discovery_evidence"
			}
		}
		if len(evidence["paths"].([]any)) == 0 {
			return nil, "invalid_legacy_discovery_evidence"
		}
		r.Platform, _ = evidence["platform"].(string)
		if r.Platform != "twitter" && r.Platform != "reddit" {
			return nil, "unsupported_legacy_discovery_platform"
		}
		if evidence["account_key"] != nil {
			r.AccountKey, ok = enrichmentLegacyString(evidence["account_key"], false, 8192)
			if !ok {
				return nil, "invalid_legacy_discovery_account"
			}
		}
		if _, ok := evidence["strict_filename_id"].(bool); !ok {
			return nil, "invalid_legacy_discovery_evidence"
		}
		if r.Status == "lookup" {
			r.Projection.CandidateURL, ok = enrichmentLegacyString(evidence["candidate_url"], false, 8192)
			scope, err := SourceScopeV1(r.Projection.CandidateURL)
			if !ok || err != nil || !archive.EnrichmentPostURL(r.Projection.CandidateURL) || scope != "service:"+r.Platform {
				return nil, "invalid_legacy_discovery_candidate_url"
			}
			r.Disposition = "lookup"
		} else {
			if evidence["candidate_url"] != nil || r.AccountKey == "" {
				return nil, "invalid_legacy_discovery_evidence"
			}
			r.Disposition = "held"
		}
	case "done", "matched":
		r.Disposition = "historical_completion"
	case "already_native":
		r.Disposition = "source_present"
	case "coalesced":
		r.Disposition = "coalesced"
	case "no_known_profile", "gone", "unmatched", "identity_conflict", "unsupported", "ambiguous":
		r.Disposition = "review"
	default:
		return nil, "unsupported_legacy_discovery_target_status"
	}
	return r, ""
}

func prepareDiscoveryCandidate(v map[string]any) (*AutomationDiscoveryPrepared, string) {
	r, reason := discoveryPost(v)
	if reason != "" {
		return nil, reason
	}
	p := &r.Projection
	var ok bool
	p.CandidateURL, ok = enrichmentLegacyString(v["url"], false, 8192)
	if !ok || !archive.EnrichmentPostURL(p.CandidateURL) {
		return nil, "invalid_legacy_discovery_candidate_url"
	}
	p.CandidateBasis, _ = v["basis"].(string)
	switch p.CandidateBasis {
	case "filename-id-confirmed", "exact-title-and-date", "exact-original-text-and-date", "exact-title-and-original-text", "exact-source-url-and-date", "title-needs-verification":
	default:
		return nil, "unsupported_legacy_discovery_candidate_basis"
	}
	_, p.PayloadSHA256, ok = discoveryObject(v["payload_json"], CatalogChunkLimit)
	if !ok {
		return nil, "invalid_legacy_discovery_candidate_payload"
	}
	r.Disposition = "candidate"
	return r, ""
}

// Keep large JSON integers exact. Maintenance summaries are historical inputs,
// never imported as live cursors, policies or proof that new work is complete.
func discoveryCounts(v any) bool {
	counts, ok := v.(map[string]any)
	if !ok || len(counts) > 256 {
		return false
	}
	for key, value := range counts {
		n, ok := automationInteger(value)
		if key == "" || len(key) > 128 || strings.ContainsRune(key, 0) || !ok || n < 0 {
			return false
		}
	}
	return true
}

func prepareDiscoveryMaintenance(v map[string]any, captured time.Time) (*AutomationDiscoveryPrepared, string) {
	key, ok := enrichmentLegacyString(v["key"], false, 128)
	if !ok {
		return nil, "invalid_legacy_maintenance_key"
	}
	raw, ok := enrichmentLegacyString(v["value"], false, CatalogChunkLimit)
	if !ok {
		return nil, "invalid_legacy_maintenance_value"
	}
	r := &AutomationDiscoveryPrepared{Disposition: "maintenance_history"}
	p := &r.Projection
	p.PayloadSHA256 = CatalogSnapshotSHA([]byte(raw))
	stamp := func(value any) bool {
		at, ok := automationTime(value)
		if !ok || at.After(captured.Add(time.Minute)) {
			return false
		}
		p.MaintenanceTime = &at
		return true
	}
	switch key {
	case "inventory-watermark-ns":
		n, valid := automationInteger(json.Number(raw))
		if !valid || n <= 0 || strings.TrimSpace(raw) != raw || !json.Valid([]byte(raw)) || time.Unix(0, n).After(captured.Add(time.Minute)) {
			return nil, "invalid_legacy_inventory_watermark"
		}
		p.MaintenanceKind, p.WatermarkNS = "inventory_watermark", raw
	case "translation-seed-v1", "translation-seed-en-v2":
		if !stamp(raw) {
			return nil, "invalid_legacy_maintenance_time"
		}
		p.MaintenanceKind = "translation_seed"
	case "metadata-enrichment-seed-v1", "metadata-discovery-seed-v1":
		value, _, valid := discoveryObject(raw, CatalogChunkLimit)
		if !valid || !discoveryCounts(value) {
			return nil, "invalid_legacy_seed_summary"
		}
		p.MaintenanceKind = "seed_summary"
	case "enrichment-excluded-sources":
		value, _, valid := discoveryObject(raw, CatalogChunkLimit)
		if !valid || !stamp(value["requested_at"]) || !discoveryStringList(value["sources"], 256) {
			return nil, "invalid_legacy_source_exclusions"
		}
		for _, item := range value["sources"].([]any) {
			if text := item.(string); text == "" || len(text) > 128 {
				return nil, "invalid_legacy_source_exclusions"
			}
		}
		p.MaintenanceKind = "source_exclusions"
	case "metadata-pruning-last-run":
		value, _, valid := discoveryObject(raw, CatalogChunkLimit)
		if !valid || !stamp(value["at"]) || !discoveryCounts(value["counts"]) || !discoveryStringList(value["affected_catalogs"], 100000) {
			return nil, "invalid_legacy_pruning_summary"
		}
		if _, ok := value["applied"].(bool); !ok {
			return nil, "invalid_legacy_pruning_summary"
		}
		for _, item := range value["affected_catalogs"].([]any) {
			if !catalogSnapshotID.MatchString(item.(string)) {
				return nil, "invalid_legacy_pruning_summary"
			}
		}
		if n, ok := automationInteger(value["ambiguous_assets_retained"]); !ok || n < 0 {
			return nil, "invalid_legacy_pruning_summary"
		}
		p.MaintenanceKind = "pruning_summary"
	default:
		return nil, "unsupported_legacy_maintenance_key"
	}
	return r, ""
}
