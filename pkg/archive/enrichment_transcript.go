package archive

import (
	"bytes"
	"encoding/json"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

const (
	EnrichmentTranscriptSchema   = "stash-metadata-fetch-v1"
	MaxEnrichmentTranscriptBytes = 32 << 20
	MaxEnrichmentExpandedBytes   = 128 << 20
	MaxEnrichmentReferences      = 256
)

type EnrichmentRecord struct {
	Kind       string         `json:"kind"`
	Base       *int           `json:"base"`
	Parent     *int           `json:"parent"`
	Patch      map[string]any `json:"patch"`
	Removed    []string       `json:"removed"`
	ObservedAt string         `json:"observed_at"`
}

type EnrichmentReference struct {
	URL    string `json:"url"`
	Parent int    `json:"parent"`
	Depth  int    `json:"depth"`
	Reason string `json:"reason"`
}

// EnrichmentTranscript preserves the producer's compact records and original
// observation times. Validation grants no authority to publish a post or finish
// a job; the coordinator must verify the target and its current owned lease.
type EnrichmentTranscript struct {
	Schema           string                `json:"schema"`
	URL              string                `json:"url"`
	RetentionPolicy  string                `json:"retention_policy"`
	ExtractorVersion string                `json:"extractor_version"`
	Records          []EnrichmentRecord    `json:"records"`
	Pending          []EnrichmentReference `json:"pending"`
	Unresolved       []EnrichmentReference `json:"unresolved"`
	body             json.RawMessage
	metadata         []sourceObject
	expanded         []sourceObject
	depths           []int
	seen             map[string]bool
}

var enrichmentObservedTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$`)

func enrichmentPublicURL(value string) bool {
	if value == "" || len(value) > 8192 || strings.ContainsFunc(value, func(r rune) bool { return r <= 32 || r == 127 }) {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > 65535 {
			return false
		}
	}
	return true
}

func enrichmentObjectKeys(value sourceObject, keys ...string) bool {
	if len(value) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := value[key]; !ok {
			return false
		}
	}
	return true
}

func enrichmentIndex(value any, before int, nullable bool) (*int, error) {
	if value == nil && nullable {
		return nil, nil
	}
	number, ok := value.(json.Number)
	if !ok {
		return nil, models.ErrEnrichmentInvalid
	}
	n, err := number.Int64()
	if err != nil || n < 0 || n >= int64(before) {
		return nil, models.ErrEnrichmentInvalid
	}
	ret := int(n)
	return &ret, nil
}

func enrichmentReasonCode(value string) bool {
	switch value {
	case "rate_limited", "authentication", "access_denied", "challenge", "not_found", "unsupported_extractor",
		"extraction_failed", "timeout", "result_too_large", "invalid_checkpoint", "not_a_post_url", "worker_failed",
		"runtime_changed", "external_reference_only":
		return true
	}
	return false
}

func (t *EnrichmentTranscript) restore(raw any, expandedBytes *int) error {
	value, ok := raw.(sourceObject)
	if !ok || !enrichmentObjectKeys(value, "kind", "base", "parent", "patch", "removed", "observed_at") {
		return models.ErrEnrichmentInvalid
	}
	record := EnrichmentRecord{Removed: []string{}}
	record.Kind, _ = value["kind"].(string)
	record.ObservedAt, _ = value["observed_at"].(string)
	if (record.Kind != "post" && record.Kind != "media" && record.Kind != "context") || !enrichmentObservedTime.MatchString(record.ObservedAt) {
		return models.ErrEnrichmentInvalid
	}
	observed, err := time.Parse(time.RFC3339Nano, record.ObservedAt)
	if err != nil || observed.IsZero() || observed.UTC().Year() < 1 || observed.UTC().Year() > 9999 {
		return models.ErrEnrichmentInvalid
	}
	index := len(t.Records)
	record.Base, err = enrichmentIndex(value["base"], index, true)
	if err != nil {
		return err
	}
	record.Parent, err = enrichmentIndex(value["parent"], index, true)
	if err != nil {
		return err
	}
	depth := 0
	if record.Parent != nil {
		depth = t.depths[*record.Parent] + 1
		if depth > 2 {
			return models.ErrEnrichmentInvalid
		}
	}
	record.Patch, ok = value["patch"].(sourceObject)
	if !ok {
		return models.ErrEnrichmentInvalid
	}
	removed, ok := value["removed"].([]any)
	if !ok {
		return models.ErrEnrichmentInvalid
	}
	data := sourceObject{}
	if record.Base != nil {
		for key, child := range t.metadata[*record.Base] {
			data[key] = child
		}
	}
	for i, raw := range removed {
		key, ok := raw.(string)
		if !ok || (i > 0 && key <= record.Removed[i-1]) {
			return models.ErrEnrichmentInvalid
		}
		_, inBase := data[key]
		_, inPatch := record.Patch[key]
		if !inBase || inPatch {
			return models.ErrEnrichmentInvalid
		}
		delete(data, key)
		record.Removed = append(record.Removed, key)
	}
	for key, child := range record.Patch {
		data[key] = child
	}
	sourceURL, _ := data["source_extractor_url"].(string)
	if !enrichmentPublicURL(sourceURL) {
		return models.ErrEnrichmentInvalid
	}
	body, err := EncodeSourceJSON(data)
	if err != nil || len(body) > MaxSourcePayloadBytes {
		return models.ErrEnrichmentInvalid
	}
	retained, err := RetainSourcePayload(body)
	if err != nil || !bytes.Equal(body, retained) {
		return models.ErrEnrichmentInvalid
	}
	parentKey := "root"
	if record.Parent != nil {
		parentKey = strconv.Itoa(*record.Parent)
	}
	key := record.Kind + "\x00" + parentKey + "\x00" + translationDigest(body)
	if t.seen[key] {
		return models.ErrEnrichmentInvalid
	}
	expanded := data
	if record.Parent != nil {
		expanded = sourceObject{}
		for key, child := range data {
			expanded[key] = child
		}
		parent := t.expanded[*record.Parent]
		key := "_parent"
		if parent["category"] == "reddit" {
			key = "_reddit"
		}
		expanded[key] = parent
		body, err = EncodeSourceJSON(expanded)
		if err != nil || len(body) > MaxSourcePayloadBytes {
			return models.ErrEnrichmentInvalid
		}
	}
	if len(body) > MaxEnrichmentExpandedBytes-*expandedBytes {
		return models.ErrEnrichmentInvalid
	}
	*expandedBytes += len(body)
	t.seen[key] = true
	t.Records = append(t.Records, record)
	t.metadata = append(t.metadata, data)
	t.expanded = append(t.expanded, expanded)
	t.depths = append(t.depths, depth)
	return nil
}

func (t *EnrichmentTranscript) references(raw any, pending bool) ([]EnrichmentReference, error) {
	values, ok := raw.([]any)
	if !ok || len(values) > MaxEnrichmentReferences {
		return nil, models.ErrEnrichmentInvalid
	}
	ret := make([]EnrichmentReference, 0, len(values))
	seen := map[EnrichmentReference]bool{}
	for _, raw := range values {
		value, ok := raw.(sourceObject)
		if !ok || !enrichmentObjectKeys(value, "url", "parent", "depth", "reason") {
			return nil, models.ErrEnrichmentInvalid
		}
		ref := EnrichmentReference{}
		ref.URL, _ = value["url"].(string)
		ref.Reason, _ = value["reason"].(string)
		if !enrichmentPublicURL(ref.URL) || !enrichmentReasonCode(ref.Reason) {
			return nil, models.ErrEnrichmentInvalid
		}
		parent, err := enrichmentIndex(value["parent"], len(t.Records), false)
		if err != nil {
			return nil, err
		}
		depth, err := enrichmentIndex(value["depth"], 4, false)
		if err != nil || *depth != t.depths[*parent]+1 || (pending && *depth > 2) {
			return nil, models.ErrEnrichmentInvalid
		}
		ref.Parent, ref.Depth = *parent, *depth
		if seen[ref] {
			return nil, models.ErrEnrichmentInvalid
		}
		seen[ref] = true
		ret = append(ret, ref)
	}
	return ret, nil
}

func ParseEnrichmentTranscript(raw []byte) (*EnrichmentTranscript, error) {
	value, err := DecodeJSONObject(raw, MaxEnrichmentTranscriptBytes)
	if err != nil || !enrichmentObjectKeys(value, "schema", "url", "retention_policy", "extractor_version", "records", "pending", "unresolved") {
		return nil, models.ErrEnrichmentInvalid
	}
	t := &EnrichmentTranscript{Records: []EnrichmentRecord{}, seen: map[string]bool{}}
	t.Schema, _ = value["schema"].(string)
	t.URL, _ = value["url"].(string)
	t.RetentionPolicy, _ = value["retention_policy"].(string)
	t.ExtractorVersion, _ = value["extractor_version"].(string)
	if t.Schema != EnrichmentTranscriptSchema || !EnrichmentPostURL(t.URL) || t.RetentionPolicy != SourceRetentionVersion ||
		t.ExtractorVersion == "" || len(t.ExtractorVersion) > 128 || strings.ContainsAny(t.ExtractorVersion, "\r\n\x00") {
		return nil, models.ErrEnrichmentInvalid
	}
	records, ok := value["records"].([]any)
	if !ok || len(records) > MaxEnrichmentCaptures {
		return nil, models.ErrEnrichmentInvalid
	}
	expandedBytes := 0
	for _, record := range records {
		if err := t.restore(record, &expandedBytes); err != nil {
			return nil, err
		}
	}
	t.Pending, err = t.references(value["pending"], true)
	if err != nil {
		return nil, err
	}
	t.Unresolved, err = t.references(value["unresolved"], false)
	if err != nil {
		return nil, err
	}
	t.body, err = EncodeSourceJSON(value)
	if err != nil || len(t.body) > MaxEnrichmentTranscriptBytes {
		return nil, models.ErrEnrichmentInvalid
	}
	return t, nil
}

func (t *EnrichmentTranscript) Body() json.RawMessage { return bytes.Clone(t.body) }

func (t *EnrichmentTranscript) Metadata(index int) (json.RawMessage, error) {
	if index < 0 || index >= len(t.expanded) {
		return nil, models.ErrEnrichmentInvalid
	}
	return EncodeSourceJSON(t.expanded[index])
}

// Extends preserves already-observed records while a retry completes children.
// A new request/runtime needs new work; it cannot rewrite a retained checkpoint.
func (t *EnrichmentTranscript) Extends(previous *EnrichmentTranscript) bool {
	if previous == nil || t.Schema != previous.Schema || t.URL != previous.URL || t.RetentionPolicy != previous.RetentionPolicy ||
		t.ExtractorVersion != previous.ExtractorVersion || len(t.Records) < len(previous.Records) ||
		!reflect.DeepEqual(t.Records[:len(previous.Records)], previous.Records) || len(t.Unresolved) < len(previous.Unresolved) ||
		!reflect.DeepEqual(t.Unresolved[:len(previous.Unresolved)], previous.Unresolved) {
		return false
	}
	for _, pending := range previous.Pending {
		found := false
		for _, refs := range [][]EnrichmentReference{t.Pending, t.Unresolved} {
			for _, ref := range refs {
				if ref.URL == pending.URL && ref.Parent == pending.Parent && ref.Depth == pending.Depth {
					found = true
				}
			}
		}
		for i := len(previous.Records); i < len(t.Records) && !found; i++ {
			parent := t.Records[i].Parent
			found = parent != nil && *parent == pending.Parent && t.metadata[i]["source_extractor_url"] == pending.URL
		}
		if !found {
			return false
		}
	}
	return true
}
