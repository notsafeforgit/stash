package scrape

import (
	"encoding/json"
	"reflect"
	"slices"

	"github.com/stashapp/stash/pkg/archive"
)

const EnrichmentStagingPolicy = "legacy-enrichment-staging-v1"
const MaxEnrichmentStagingBytes = 8 << 20

// This is retained migration evidence, not a native worker transcript. The old
// collector did not record observation times or a producer/lease identity.
type EnrichmentStaging struct {
	Format               string                      `json:"format"`
	ObservationTimeBasis string                      `json:"observation_time_basis"`
	ExtractorVersion     *string                     `json:"reported_extractor_version"`
	Bodies               []EnrichmentStagedBody      `json:"bodies"`
	Records              []EnrichmentStagedRecord    `json:"records"`
	Pending              []EnrichmentStagedPending   `json:"pending"`
	Unresolved           []EnrichmentStagedReference `json:"unresolved"`
	Error                *string                     `json:"error"`
	RetryReason          *string                     `json:"retry_reason"`
}

type EnrichmentStagedBody struct {
	Base    *int           `json:"base"`
	Patch   map[string]any `json:"patch"`
	Removed []string       `json:"removed"`
	Parents map[string]int `json:"parents"`
}

type EnrichmentStagedRecord struct {
	Kind string `json:"kind"`
	Body int    `json:"body"`
}

type EnrichmentStagedReference struct {
	URL    string  `json:"url"`
	Reason *string `json:"reason"`
}

type EnrichmentStagedPending struct {
	EnrichmentStagedReference
	Parent int `json:"parent"`
	Depth  int `json:"depth"`
}

func stagingKeys(value map[string]any, required []string, optional ...string) bool {
	for _, key := range required {
		if _, found := value[key]; !found {
			return false
		}
	}
	for key := range value {
		if !slices.Contains(required, key) && !slices.Contains(optional, key) {
			return false
		}
	}
	return true
}

type stagingBuilder struct {
	result EnrichmentStaging
	seen   map[string]int
	flat   []map[string]any
}

func (b *stagingBuilder) body(raw any, depth int) (int, bool) {
	data, ok := raw.(map[string]any)
	if !ok || depth > 2 || len(b.result.Bodies) >= 4096 {
		return 0, false
	}
	encoded, err := archive.EncodeSourceJSON(data)
	if err != nil {
		return 0, false
	}
	// Retain the old stored payload exactly, including its reduction policy.
	// Reapplying today's policy here could change the meaning of a saved retry.
	key := CatalogSnapshotSHA(encoded)
	if index, found := b.seen[key]; found {
		return index, true
	}
	flat := make(map[string]any, len(data))
	for key, value := range data {
		flat[key] = value
	}
	parents := map[string]int{}
	for _, key := range []string{"_parent", "_reddit"} {
		if parent, found := flat[key]; found {
			index, valid := b.body(parent, depth+1)
			if !valid {
				return 0, false
			}
			parents[key] = index
			delete(flat, key)
		}
	}
	item := EnrichmentStagedBody{Patch: flat, Removed: []string{}, Parents: parents}
	// Use a preceding body only when its exact shallow delta saves bytes. This
	// shares repeated captions/profile objects without flattening parent context.
	if index := len(b.flat) - 1; index >= 0 {
		patch, removed := map[string]any{}, []string{}
		for key, value := range flat {
			before, exists := b.flat[index][key]
			if !exists || !reflect.DeepEqual(before, value) {
				patch[key] = value
			}
		}
		for key := range b.flat[index] {
			if _, exists := flat[key]; !exists {
				removed = append(removed, key)
			}
		}
		slices.Sort(removed)
		delta := EnrichmentStagedBody{Base: &index, Patch: patch, Removed: removed, Parents: parents}
		plain, err := json.Marshal(item)
		if err != nil {
			return 0, false
		}
		compact, err := json.Marshal(delta)
		if err != nil {
			return 0, false
		}
		if len(compact) < len(plain) {
			item = delta
		}
	}
	index := len(b.result.Bodies)
	b.result.Bodies = append(b.result.Bodies, item)
	b.flat = append(b.flat, flat)
	b.seen[key] = index
	return index, true
}

func stagingList(value map[string]any, key string) ([]any, bool) {
	raw, exists := value[key]
	if !exists {
		return []any{}, true
	}
	list, ok := raw.([]any)
	return list, ok && len(list) <= 256
}

func stagingReference(value map[string]any) (EnrichmentStagedReference, bool) {
	url, ok := enrichmentLegacyString(value["url"], false, 8192)
	if !ok {
		return EnrichmentStagedReference{}, false
	}
	// This preserves evidence; URL admission and supported child routing belong
	// to reviewed execution. Never silently remove an unknown external reference.
	reason, ok := enrichmentLegacyOptional(value["reason"], 128)
	return EnrichmentStagedReference{URL: url, Reason: reason}, ok
}

// ConvertEnrichmentStaging has no dependency on an installed old package. All
// numbers retain their JSON spelling, all original record slots remain, and
// unknown formats remain review evidence rather than being partially converted.
func ConvertEnrichmentStaging(raw []byte) (*EnrichmentStaging, json.RawMessage, string) {
	value, err := archive.DecodeJSONObject(raw, CatalogChunkLimit)
	if err != nil {
		return nil, nil, "invalid_legacy_checkpoint_json"
	}
	if !stagingKeys(value, []string{"records"}, "unresolved", "pending_children", "extractor_version", "error", "retry_reason") {
		return nil, nil, "unsupported_legacy_checkpoint_fields"
	}
	b := stagingBuilder{seen: map[string]int{}, result: EnrichmentStaging{Format: EnrichmentStagingPolicy,
		ObservationTimeBasis: "unrecorded", Bodies: []EnrichmentStagedBody{}, Records: []EnrichmentStagedRecord{},
		Pending: []EnrichmentStagedPending{}, Unresolved: []EnrichmentStagedReference{}}}
	for _, field := range []struct {
		name string
		out  **string
	}{
		{"extractor_version", &b.result.ExtractorVersion}, {"error", &b.result.Error}, {"retry_reason", &b.result.RetryReason},
	} {
		text, ok := enrichmentLegacyOptional(value[field.name], 128)
		if !ok {
			return nil, nil, "invalid_legacy_checkpoint_header"
		}
		*field.out = text
	}
	records, ok := stagingList(value, "records")
	if !ok || len(records) == 0 {
		return nil, nil, "invalid_legacy_checkpoint_records"
	}
	for _, raw := range records {
		record, ok := raw.(map[string]any)
		if !ok || !stagingKeys(record, []string{"kind", "metadata"}) {
			return nil, nil, "invalid_legacy_checkpoint_record"
		}
		kind, ok := record["kind"].(string)
		if !ok || (kind != "post" && kind != "media") {
			return nil, nil, "unsupported_legacy_checkpoint_kind"
		}
		index, ok := b.body(record["metadata"], 0)
		if !ok {
			return nil, nil, "invalid_legacy_checkpoint_metadata"
		}
		b.result.Records = append(b.result.Records, EnrichmentStagedRecord{Kind: kind, Body: index})
	}
	pending, ok := stagingList(value, "pending_children")
	if !ok {
		return nil, nil, "invalid_legacy_checkpoint_pending"
	}
	for _, raw := range pending {
		item, ok := raw.(map[string]any)
		if !ok || !stagingKeys(item, []string{"url", "parent", "depth"}, "reason") {
			return nil, nil, "invalid_legacy_checkpoint_pending"
		}
		ref, valid := stagingReference(item)
		depth, depthOK := automationInteger(item["depth"])
		index, parentOK := b.body(item["parent"], 0)
		if !valid || !depthOK || depth < 1 || depth > 2 || !parentOK {
			return nil, nil, "invalid_legacy_checkpoint_pending"
		}
		b.result.Pending = append(b.result.Pending, EnrichmentStagedPending{EnrichmentStagedReference: ref, Parent: index, Depth: int(depth)})
	}
	unresolved, ok := stagingList(value, "unresolved")
	if !ok {
		return nil, nil, "invalid_legacy_checkpoint_unresolved"
	}
	for _, raw := range unresolved {
		item, ok := raw.(map[string]any)
		if !ok || !stagingKeys(item, []string{"url"}, "reason") {
			return nil, nil, "invalid_legacy_checkpoint_unresolved"
		}
		ref, ok := stagingReference(item)
		if !ok {
			return nil, nil, "invalid_legacy_checkpoint_unresolved"
		}
		b.result.Unresolved = append(b.result.Unresolved, ref)
	}
	encoded, err := archive.EncodeSourceJSON(b.result)
	if err != nil {
		return nil, nil, "invalid_legacy_checkpoint_projection"
	}
	tree, err := archive.DecodeJSONObject(encoded, MaxEnrichmentStagingBytes)
	if err != nil {
		return nil, nil, "legacy_checkpoint_projection_too_large"
	}
	body, err := archive.EncodeSourceJSON(tree)
	if err != nil {
		return nil, nil, "invalid_legacy_checkpoint_projection"
	}
	return &b.result, body, ""
}
