package scrape

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
)

const AutomationEnrichmentPolicy = "automation-enrichment-v1"

// These are migration projections, not native execution acknowledgements. The
// importer must resolve retained post/collection identities and the named proof
// before recording a completed or coalesced outcome. Original values and staged
// bytes stay in the immutable automation snapshot.
type AutomationEnrichmentJob struct {
	CatalogID, PostKey, Platform, Status string
	Version, Priority                    int
	Attempts                             int64
	AccountKey, URL                      *string
	NotBefore                            time.Time
	CreatedAt, UpdatedAt                 time.Time
	ServiceScope                         string
	Disposition                          string
	EvidenceRequired                     string
	StagedSHA256                         string
}

type AutomationEnrichmentCooldown struct {
	Kind, Value, Reason string
	Until               time.Time
}

type AutomationEnrichmentSeed struct {
	CatalogID, LastPostKey string
	Complete               bool
	Counts                 map[string]int64
}

type AutomationEnrichmentSourceProgress struct {
	Platform    string
	LastAttempt time.Time
}

type CatalogEnrichmentReceipt struct {
	PostKey                 string
	Version                 int
	CompletedAt             time.Time
	AttachmentLinksEnriched int64
	UnresolvedChildren      int64
}

func enrichmentLegacyString(value any, allowEmpty bool, maxBytes int) (string, bool) {
	text, ok := value.(string)
	return text, ok && (allowEmpty || text != "") && len(text) <= maxBytes && !strings.ContainsRune(text, 0)
}

func enrichmentLegacyOptional(value any, maxBytes int) (*string, bool) {
	if value == nil {
		return nil, true
	}
	text, ok := enrichmentLegacyString(value, false, maxBytes)
	return &text, ok
}

func validEnrichmentBoundary(captured time.Time) bool {
	return !captured.IsZero() && captured.UnixMilli() > 0 && captured.Year() <= 9999
}

// Avoid a float conversion before rounding: at epoch-sized values, even a
// sub-microsecond remainder can disappear and move a retry one millisecond early.
func enrichmentLegacyDeadline(value any, captured time.Time) (time.Time, bool) {
	number, ok := value.(json.Number)
	if !ok || len(number) > 128 || !json.Valid([]byte(number)) {
		return time.Time{}, false
	}
	if at := strings.IndexAny(string(number), "eE"); at >= 0 {
		exponent, err := strconv.Atoi(string(number)[at+1:])
		if err != nil || exponent < -400 || exponent > 400 {
			return time.Time{}, false
		}
	}
	seconds, ok := new(big.Rat).SetString(string(number))
	if !ok || seconds.Sign() < 0 || seconds.Cmp(big.NewRat(253402300800, 1)) >= 0 {
		return time.Time{}, false
	}
	if seconds.Sign() == 0 {
		return captured.UTC(), true
	}
	numerator := new(big.Int).Mul(seconds.Num(), big.NewInt(1000))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, seconds.Denom(), remainder)
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	stamp := time.UnixMilli(quotient.Int64()).UTC()
	return stamp, stamp.Year() <= 9999
}

func PrepareAutomationEnrichmentJob(values map[string]any, captured time.Time) (*AutomationEnrichmentJob, string) {
	ret := &AutomationEnrichmentJob{}
	var ok bool
	ret.CatalogID, ok = values["catalog_id"].(string)
	if !ok || !catalogSnapshotID.MatchString(ret.CatalogID) {
		return nil, "invalid_legacy_catalog"
	}
	ret.PostKey, ok = enrichmentLegacyString(values["post_key"], false, 16384)
	if !ok {
		return nil, "invalid_legacy_post_key"
	}
	version, ok := automationInteger(values["version"])
	if !ok || version != 1 {
		return nil, "unsupported_legacy_enrichment_version"
	}
	ret.Version = int(version)
	ret.Platform, ok = enrichmentLegacyString(values["platform"], false, 128)
	if !ok {
		return nil, "invalid_legacy_platform"
	}
	ret.AccountKey, ok = enrichmentLegacyOptional(values["account_key"], 8192)
	if !ok {
		return nil, "invalid_legacy_account_key"
	}
	ret.URL, ok = enrichmentLegacyOptional(values["url"], 8192)
	if !ok {
		return nil, "invalid_legacy_url"
	}
	priority, priorityOK := automationInteger(values["priority"])
	attempts, attemptsOK := automationInteger(values["attempts"])
	deadline, deadlineOK := enrichmentLegacyDeadline(values["next_attempt"], captured)
	created, createdOK := automationTime(values["created_at"])
	updated, updatedOK := automationTime(values["updated_at"])
	if !validEnrichmentBoundary(captured) || !priorityOK || priority < 0 || priority > 100 || !attemptsOK || attempts < 0 ||
		!deadlineOK || !createdOK || !updatedOK || updated.Before(created) || updated.After(captured.Add(time.Minute)) {
		return nil, "invalid_legacy_schedule"
	}
	ret.Priority, ret.Attempts, ret.NotBefore = int(priority), attempts, deadline
	ret.CreatedAt, ret.UpdatedAt = created, updated
	if values["last_error"] != nil {
		if _, ok := enrichmentLegacyString(values["last_error"], true, CatalogChunkLimit); !ok {
			return nil, "invalid_legacy_error"
		}
	}
	ret.Status, ok = values["status"].(string)
	if !ok {
		return nil, "unsupported_legacy_enrichment_status"
	}
	switch ret.Status {
	case "pending", "retry":
		ret.Disposition = "held"
	case "done":
		ret.Disposition, ret.EvidenceRequired = "historical_completion", "catalog_receipt"
	case "already_native":
		ret.Disposition, ret.EvidenceRequired = "source_present", "gallery_dl_capture"
	case "coalesced":
		ret.Disposition, ret.EvidenceRequired = "coalesced", "post_alias"
	case "excluded_source":
		ret.Disposition = "excluded"
	case "no_post_url", "gone", "unsupported", "unmatched", "identity_conflict":
		ret.Disposition = "review"
	default:
		return nil, "unsupported_legacy_enrichment_status"
	}
	// Excluded and unresolved work stays explicit even when its URL is missing
	// or unsupported. Neither an old status nor a parseable URL grants execution.
	if ret.URL != nil {
		ret.ServiceScope, _ = SourceScopeV1(*ret.URL)
	}
	if ret.Disposition == "held" && (ret.URL == nil || !archive.EnrichmentPostURL(*ret.URL)) {
		return nil, "unsupported_legacy_enrichment_url"
	}
	if values["staged_json"] != nil {
		body, ok := values["staged_json"].(string)
		if !ok || body == "" || len(body) > CatalogChunkLimit {
			return nil, "invalid_legacy_enrichment_staging"
		}
		if _, err := archive.DecodeJSONObject([]byte(body), CatalogChunkLimit); err != nil {
			return nil, "invalid_legacy_enrichment_staging"
		}
		ret.StagedSHA256 = CatalogSnapshotSHA([]byte(body))
		// The legacy transcript has a different contract and may not record
		// original capture times. Never present it as a native owned checkpoint,
		// discard it to refetch, or mark it delivered based on a job label.
		ret.Disposition, ret.EvidenceRequired = "staged_review", "legacy_checkpoint_conversion"
	}
	return ret, ""
}

func PrepareAutomationEnrichmentCooldown(values map[string]any, captured time.Time) (*AutomationEnrichmentCooldown, string) {
	scope, scopeOK := enrichmentLegacyString(values["scope"], false, 8192)
	reason, reasonOK := enrichmentLegacyString(values["reason"], false, 128)
	until, untilOK := enrichmentLegacyDeadline(values["until_time"], captured)
	if !validEnrichmentBoundary(captured) || !scopeOK || !reasonOK || !untilOK {
		return nil, "invalid_legacy_cooldown"
	}
	kind, value, split := strings.Cut(scope, ":")
	if !split || value == "" || (kind != "platform" && kind != "account") || (kind == "platform" && len(value) > 128) {
		return nil, "invalid_legacy_cooldown_scope"
	}
	switch reason {
	case "rate_limited", "authentication", "challenge", "timeout", "extractor_process_failed", "extraction_failed", "access_denied":
	default:
		return nil, "unsupported_legacy_cooldown_reason"
	}
	// Retain the scope's original meaning. A platform label such as onlyfans
	// can describe a mirror; an account denial is not a service-wide outage.
	return &AutomationEnrichmentCooldown{Kind: kind, Value: value, Reason: reason, Until: until}, ""
}

// EnrichmentNotBefore preserves all applicable frozen delays without shortening
// an existing native deadline. It does not activate a target or a service pause.
func EnrichmentNotBefore(job AutomationEnrichmentJob, cooldowns []AutomationEnrichmentCooldown, native time.Time) time.Time {
	ret := job.NotBefore
	if native.After(ret) {
		ret = native
	}
	for _, cooldown := range cooldowns {
		matches := cooldown.Kind == "platform" && cooldown.Value == job.Platform
		matches = matches || (cooldown.Kind == "account" && job.AccountKey != nil && cooldown.Value == *job.AccountKey)
		if matches && cooldown.Until.After(ret) {
			ret = cooldown.Until
		}
	}
	return ret
}

func PrepareAutomationEnrichmentSeed(values map[string]any) (*AutomationEnrichmentSeed, string) {
	catalog, catalogOK := values["catalog_id"].(string)
	key, keyOK := enrichmentLegacyString(values["last_post_key"], true, 16384)
	complete, completeOK := automationInteger(values["complete"])
	raw, rawOK := values["counts_json"].(string)
	if !catalogOK || !catalogSnapshotID.MatchString(catalog) || !keyOK || !completeOK || (complete != 0 && complete != 1) || !rawOK {
		return nil, "invalid_legacy_enrichment_seed"
	}
	counts, err := archive.DecodeJSONObject([]byte(raw), 65536)
	if err != nil || len(counts) > 256 {
		return nil, "invalid_legacy_enrichment_seed_counts"
	}
	ret := &AutomationEnrichmentSeed{CatalogID: catalog, LastPostKey: key, Complete: complete == 1, Counts: map[string]int64{}}
	for key, value := range counts {
		n, ok := automationInteger(value)
		if key == "" || len(key) > 128 || !ok || n < 0 {
			return nil, "invalid_legacy_enrichment_seed_counts"
		}
		ret.Counts[key] = n
	}
	return ret, ""
}

func PrepareAutomationEnrichmentSourceProgress(values map[string]any, captured time.Time) (*AutomationEnrichmentSourceProgress, string) {
	platform, platformOK := enrichmentLegacyString(values["platform"], false, 128)
	number, numberOK := values["last_attempt"].(json.Number)
	last, lastOK := enrichmentLegacyDeadline(values["last_attempt"], captured)
	if !validEnrichmentBoundary(captured) || !platformOK || !numberOK || !lastOK || last.After(captured.Add(time.Minute)) {
		return nil, "invalid_legacy_enrichment_source_progress"
	}
	if seconds, err := number.Float64(); err != nil || seconds <= 0 {
		return nil, "invalid_legacy_enrichment_source_progress"
	}
	return &AutomationEnrichmentSourceProgress{Platform: platform, LastAttempt: last}, ""
}

func PrepareCatalogEnrichmentReceipt(values map[string]any, captured time.Time) (*CatalogEnrichmentReceipt, string) {
	key, keyOK := enrichmentLegacyString(values["post_key"], false, 16384)
	version, versionOK := automationInteger(values["version"])
	completed, completedOK := automationTime(values["completed_at"])
	raw, rawOK := values["details_json"].(string)
	if !validEnrichmentBoundary(captured) || !keyOK || !versionOK || version != 1 || !completedOK || completed.After(captured.Add(time.Minute)) || !rawOK {
		return nil, "invalid_legacy_enrichment_receipt"
	}
	details, err := archive.DecodeJSONObject([]byte(raw), 65536)
	if err != nil || len(details) != 2 {
		return nil, "invalid_legacy_enrichment_receipt_details"
	}
	links, linksOK := automationInteger(details["attachment_links_enriched"])
	unresolved, unresolvedOK := automationInteger(details["unresolved_children"])
	if !linksOK || !unresolvedOK || links < 0 || unresolved < 0 {
		return nil, "invalid_legacy_enrichment_receipt_details"
	}
	return &CatalogEnrichmentReceipt{PostKey: key, Version: int(version), CompletedAt: completed,
		AttachmentLinksEnriched: links, UnresolvedChildren: unresolved}, ""
}
