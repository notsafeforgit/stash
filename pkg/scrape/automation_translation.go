package scrape

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const AutomationTranslationPolicy = "automation-translations-v1"

// Source values remain in the immutable snapshot. This projection describes
// their native meaning without inventing provider times or execution attempts.
type AutomationTranslationJob struct {
	Key         string
	Request     models.TranslationRequestInput
	Cache       *models.TranslationCacheInput
	Priority    int
	Attempts    int64
	NotBefore   time.Time
	Status      string
	Disposition string
}

type AutomationTranslationTarget struct {
	JobKey, CatalogID, PostKey, Field string
	Applied                           bool
}

func automationInteger(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	n, err := number.Int64()
	return n, err == nil
}

func automationTime(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok || text == "" {
		return time.Time{}, false
	}
	stamp, err := time.Parse(time.RFC3339Nano, text)
	return stamp, err == nil && stamp.UnixMilli() > 0 && stamp.UTC().Year() <= 9999
}

func automationRetry(value any, captured time.Time) (time.Time, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return time.Time{}, false
	}
	seconds, err := number.Float64()
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds >= 253402300800 {
		return time.Time{}, false
	}
	if seconds == 0 {
		return captured.UTC(), true // Already due at the frozen boundary, but still held.
	}
	// A fractional legacy retry deadline must never be rounded earlier.
	stamp := time.UnixMilli(int64(math.Ceil(seconds * 1000))).UTC()
	return stamp, stamp.UnixMilli() > 0 && stamp.Year() <= 9999
}

func PrepareAutomationTranslationJob(values map[string]any, captured time.Time) (*AutomationTranslationJob, string) {
	ret := &AutomationTranslationJob{Disposition: "request"}
	var ok bool
	ret.Key, ok = values["job_key"].(string)
	if !ok || !archive.ValidSHA256(ret.Key) {
		return nil, "invalid_job_key"
	}
	original, originalOK := values["original_text"].(string)
	target, targetOK := values["target_language"].(string)
	if !originalOK || !targetOK {
		return nil, "invalid_request_values"
	}
	// This historical worker always invoked its provider and published evidence
	// in English. Other declarations need review, not a fabricated policy match.
	if target != "en" {
		return nil, "unsupported_legacy_target_language"
	}
	ret.Request = models.TranslationRequestInput{OriginalText: original, TargetLanguage: target, Policy: models.TranslationBingTextV1}
	request, err := archive.PrepareTranslationRequest(ret.Request)
	if err != nil {
		return nil, "invalid_request_values"
	}
	identity, err := LegacyCatalogJSON([]any{original, target}, 6*archive.MaxTranslationTextBytes+512)
	if err != nil || CatalogSnapshotSHA(identity) != ret.Key {
		return nil, "legacy_job_hash_mismatch"
	}
	priority, priorityOK := automationInteger(values["priority"])
	attempts, attemptsOK := automationInteger(values["attempts"])
	deadline, deadlineOK := automationRetry(values["next_attempt"], captured)
	created, createdOK := automationTime(values["created_at"])
	updated, updatedOK := automationTime(values["updated_at"])
	if !priorityOK || priority < 0 || priority > 100 || !attemptsOK || attempts < 0 || !deadlineOK ||
		!createdOK || !updatedOK || updated.Before(created) || updated.After(captured.Add(time.Minute)) {
		return nil, "invalid_legacy_schedule"
	}
	ret.Priority, ret.Attempts, ret.NotBefore = int(priority), attempts, deadline
	for _, name := range []string{"source_hint", "last_error"} {
		if value := values[name]; value != nil {
			if _, ok := value.(string); !ok {
				return nil, "invalid_legacy_job_values"
			}
		}
	}
	ret.Status, ok = values["status"].(string)
	if !ok || (ret.Status != "pending" && ret.Status != "retry" && ret.Status != "done" && ret.Status != "preserved") {
		return nil, "unsupported_legacy_job_status"
	}
	var result string
	if values["result_json"] != nil {
		result, ok = values["result_json"].(string)
		if !ok {
			return nil, "invalid_legacy_cache"
		}
	}
	if result == "" {
		if ret.Status == "done" {
			return nil, "completed_cache_missing"
		}
		return ret, ""
	}
	object, err := archive.DecodeJSONObject([]byte(result), CatalogChunkLimit)
	if err != nil || len(object) != 4 {
		return nil, "invalid_legacy_cache"
	}
	cache := &models.TranslationCacheInput{RequestUUID: request.UUID, Origin: "migration"}
	for name, destination := range map[string]**string{"translated_text": &cache.TranslatedText, "source_language": &cache.SourceLanguage, "provider": &cache.Provider} {
		value, exists := object[name]
		if !exists {
			return nil, "invalid_legacy_cache"
		}
		if value != nil {
			text, ok := value.(string)
			if !ok {
				return nil, "invalid_legacy_cache"
			}
			*destination = &text
		}
	}
	ret.Disposition = "cache"
	switch object["status"] {
	case "translated":
		cache.Status = "translated"
	case "english":
		cache.Status = "unchanged"
		if cache.TranslatedText == nil || cache.SourceLanguage == nil || strings.Split(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(*cache.SourceLanguage)), "_", "-"), "-")[0] != "en" {
			return nil, "unproven_english_cache"
		}
		if *cache.TranslatedText != original {
			// Older results sometimes rewrote English. The legacy application did
			// not publish those rewrites; retain them in the source and explicitly
			// preserve the original as the native unchanged outcome.
			ret.Disposition = "english_original"
			cache.TranslatedText = &original
		}
	case "no-text":
		cache.Status = "no_text"
	default:
		return nil, "unsupported_legacy_cache_status"
	}
	if cache.Status != "no_text" && (cache.Provider == nil || *cache.Provider != "translate-shell/bing") {
		return nil, "unsupported_legacy_provider"
	}
	if _, _, err := archive.PrepareTranslationCache(request, *cache); err != nil {
		return nil, "invalid_legacy_cache"
	}
	ret.Cache = cache
	return ret, ""
}

func PrepareAutomationTranslationTarget(values map[string]any) (*AutomationTranslationTarget, string) {
	ret := &AutomationTranslationTarget{}
	for name, target := range map[string]*string{"job_key": &ret.JobKey, "catalog_id": &ret.CatalogID, "post_key": &ret.PostKey, "field": &ret.Field} {
		value, ok := values[name].(string)
		if !ok || value == "" {
			return nil, "invalid_target_values"
		}
		*target = value
	}
	applied, ok := automationInteger(values["applied"])
	if !ok || (applied != 0 && applied != 1) || !archive.ValidSHA256(ret.JobKey) || !catalogSnapshotID.MatchString(ret.CatalogID) ||
		(ret.Field != "title" && ret.Field != "caption") {
		return nil, "invalid_target_values"
	}
	ret.Applied = applied == 1
	return ret, ""
}
