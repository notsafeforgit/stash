package archive

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func enrichmentTranscriptFixture(t *testing.T) (json.RawMessage, json.RawMessage, []json.RawMessage) {
	t.Helper()
	body, err := os.ReadFile("testdata/enrichment-transcript-v1.json")
	require.NoError(t, err)
	var fixture struct {
		Initial  json.RawMessage   `json:"initial"`
		Complete json.RawMessage   `json:"complete"`
		Metadata []json.RawMessage `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal(body, &fixture))
	return fixture.Initial, fixture.Complete, fixture.Metadata
}

func TestEnrichmentTranscriptReconstructsProducerCheckpoint(t *testing.T) {
	initial, complete, expected := enrichmentTranscriptFixture(t)
	prior, err := ParseEnrichmentTranscript(initial)
	require.NoError(t, err)
	result, err := ParseEnrichmentTranscript(complete)
	require.NoError(t, err)
	require.True(t, result.Extends(prior))
	require.Len(t, prior.Pending, 1)
	require.Empty(t, result.Pending)
	require.Len(t, result.Unresolved, 1)
	require.Equal(t, []string{"legacy_field"}, result.Records[1].Removed)
	for i, raw := range expected {
		value, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
		require.NoError(t, err)
		canonical, err := EncodeSourceJSON(value)
		require.NoError(t, err)
		actual, err := result.Metadata(i)
		require.NoError(t, err)
		require.Equal(t, canonical, actual, "record %d", i)
	}
	root, err := result.Metadata(0)
	require.NoError(t, err)
	value, err := DecodeJSONObject(root, MaxSourcePayloadBytes)
	require.NoError(t, err)
	require.Equal(t, json.Number("9223372036854775815"), value["large_id"])
	replayed, err := ParseEnrichmentTranscript(result.Body())
	require.NoError(t, err)
	require.Equal(t, result.Body(), replayed.Body())
	require.True(t, replayed.Extends(result))
	_, err = result.Metadata(-1)
	require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	_, err = result.Metadata(len(result.Records))
	require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
}

func TestEnrichmentTranscriptRejectsMalformedOrUnretainedEvidence(t *testing.T) {
	_, raw, _ := enrichmentTranscriptFixture(t)
	first := func(value sourceObject) sourceObject { return value["records"].([]any)[0].(sourceObject) }
	for name, change := range map[string]func(sourceObject){
		"unknown envelope field": func(v sourceObject) { v["settings"] = sourceObject{} },
		"missing envelope field": func(v sourceObject) { delete(v, "pending") },
		"unsupported schema":     func(v sourceObject) { v["schema"] = "next" },
		"unsupported retention":  func(v sourceObject) { v["retention_policy"] = "none" },
		"profile URL":            func(v sourceObject) { v["url"] = "https://www.reddit.com/user/example" },
		"extractor log":          func(v sourceObject) { v["extractor_version"] = "version\nlog" },
		"records not array":      func(v sourceObject) { v["records"] = nil },
		"record limit":           func(v sourceObject) { v["records"] = make([]any, MaxEnrichmentCaptures+1) },
		"unknown record field":   func(v sourceObject) { first(v)["extra"] = true },
		"missing record field":   func(v sourceObject) { delete(first(v), "base") },
		"record kind":            func(v sourceObject) { first(v)["kind"] = "download" },
		"base cycle":             func(v sourceObject) { first(v)["base"] = 0 },
		"base fraction":          func(v sourceObject) { first(v)["base"] = json.Number("0.0") },
		"parent cycle":           func(v sourceObject) { first(v)["parent"] = 0 },
		"boolean reference":      func(v sourceObject) { first(v)["parent"] = false },
		"not a patch":            func(v sourceObject) { first(v)["patch"] = nil },
		"missing removed list":   func(v sourceObject) { first(v)["removed"] = nil },
		"nonexistent removal":    func(v sourceObject) { first(v)["removed"] = []any{"not there"} },
		"duplicate removal": func(v sourceObject) {
			v["records"].([]any)[1].(sourceObject)["removed"] = []any{"legacy_field", "legacy_field"}
		},
		"patch also removes": func(v sourceObject) {
			v["records"].([]any)[1].(sourceObject)["patch"].(sourceObject)["legacy_field"] = nil
		},
		"timezone missing":  func(v sourceObject) { first(v)["observed_at"] = "2026-10-03T01:00:00" },
		"clock missing":     func(v sourceObject) { first(v)["observed_at"] = "2026-10-03" },
		"invalid timezone":  func(v sourceObject) { first(v)["observed_at"] = "2026-10-03T01:00:00+01:60" },
		"zero observation":  func(v sourceObject) { first(v)["observed_at"] = "0001-01-01T00:00:00Z" },
		"UTC underflow":     func(v sourceObject) { first(v)["observed_at"] = "0001-01-01T00:00:00+01:00" },
		"UTC overflow":      func(v sourceObject) { first(v)["observed_at"] = "9999-12-31T23:30:00-01:00" },
		"unretained secret": func(v sourceObject) { first(v)["patch"].(sourceObject)["cookies"] = "secret" },
		"private source URL": func(v sourceObject) {
			first(v)["patch"].(sourceObject)["source_extractor_url"] = "https://user:secret@example.invalid/post"
		},
		"missing source URL":       func(v sourceObject) { delete(first(v)["patch"].(sourceObject), "source_extractor_url") },
		"duplicate record":         func(v sourceObject) { v["records"] = append(v["records"].([]any), first(v)) },
		"unknown reference field":  func(v sourceObject) { v["unresolved"].([]any)[0].(sourceObject)["extra"] = true },
		"reference has no parent":  func(v sourceObject) { v["unresolved"].([]any)[0].(sourceObject)["parent"] = nil },
		"reference wrong depth":    func(v sourceObject) { v["unresolved"].([]any)[0].(sourceObject)["depth"] = 2 },
		"unknown reference reason": func(v sourceObject) { v["unresolved"].([]any)[0].(sourceObject)["reason"] = "raw stderr" },
		"duplicate reference":      func(v sourceObject) { v["unresolved"] = append(v["unresolved"].([]any), v["unresolved"].([]any)[0]) },
		"reference limit":          func(v sourceObject) { v["pending"] = make([]any, MaxEnrichmentReferences+1) },
	} {
		t.Run(name, func(t *testing.T) {
			value, err := DecodeJSONObject(raw, MaxEnrichmentTranscriptBytes)
			require.NoError(t, err)
			change(value)
			body, err := EncodeSourceJSON(value)
			require.NoError(t, err)
			_, err = ParseEnrichmentTranscript(body)
			require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
		})
	}
	_, err := ParseEnrichmentTranscript(append([]byte(`{"schema":"duplicate",`), raw[1:]...))
	require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
}

func TestEnrichmentTranscriptPreservesTimestampPrecision(t *testing.T) {
	initial, _, _ := enrichmentTranscriptFixture(t)
	for _, stamp := range []string{"2026-10-03T01:00:00.123456789Z", "0001-01-01T00:00:00.000000001Z", "0001-01-01T01:00:00.000000001+01:00"} {
		t.Run(stamp, func(t *testing.T) {
			value, err := DecodeJSONObject(initial, MaxEnrichmentTranscriptBytes)
			require.NoError(t, err)
			value["records"].([]any)[0].(sourceObject)["observed_at"] = stamp
			body, err := EncodeSourceJSON(value)
			require.NoError(t, err)
			parsed, err := ParseEnrichmentTranscript(body)
			require.NoError(t, err)
			require.Equal(t, stamp, parsed.Records[0].ObservedAt)
		})
	}
}

func TestEnrichmentTranscriptRetriesCannotEraseRetainedEvidence(t *testing.T) {
	initial, complete, _ := enrichmentTranscriptFixture(t)
	prior, err := ParseEnrichmentTranscript(initial)
	require.NoError(t, err)
	for name, change := range map[string]func(sourceObject){
		"root caption changed": func(v sourceObject) {
			v["records"].([]any)[0].(sourceObject)["patch"].(sourceObject)["title"] = "changed"
		},
		"observation time changed": func(v sourceObject) { v["records"].([]any)[0].(sourceObject)["observed_at"] = "2026-10-03T02:00:00Z" },
		"runtime changed":          func(v sourceObject) { v["extractor_version"] = "future" },
		"lost unresolved evidence": func(v sourceObject) { v["unresolved"] = []any{} },
		"pending child dropped":    func(v sourceObject) { v["records"] = v["records"].([]any)[:len(prior.Records)] },
		"different child returned": func(v sourceObject) {
			v["records"].([]any)[3].(sourceObject)["patch"].(sourceObject)["source_extractor_url"] = "https://www.redgifs.com/ifr/different"
		},
	} {
		t.Run(name, func(t *testing.T) {
			value, err := DecodeJSONObject(complete, MaxEnrichmentTranscriptBytes)
			require.NoError(t, err)
			change(value)
			body, err := EncodeSourceJSON(value)
			require.NoError(t, err)
			current, err := ParseEnrichmentTranscript(body)
			require.NoError(t, err)
			require.False(t, current.Extends(prior))
		})
	}
}

func TestEnrichmentTranscriptBoundsCompactExpansionAndParents(t *testing.T) {
	_, raw, _ := enrichmentTranscriptFixture(t)
	value, err := DecodeJSONObject(raw, MaxEnrichmentTranscriptBytes)
	require.NoError(t, err)
	root := value["records"].([]any)[0].(sourceObject)
	root["patch"].(sourceObject)["large_text"] = strings.Repeat("x", 2<<20)
	records := []any{root}
	for i := 1; i <= 64; i++ {
		records = append(records, sourceObject{"kind": "media", "base": 0, "parent": nil, "patch": sourceObject{"num": i}, "removed": []any{}, "observed_at": root["observed_at"]})
	}
	value["records"], value["pending"], value["unresolved"] = records, []any{}, []any{}
	body, err := EncodeSourceJSON(value)
	require.NoError(t, err)
	require.Less(t, len(body), MaxEnrichmentTranscriptBytes, "compact size alone is insufficient")
	_, err = ParseEnrichmentTranscript(body)
	require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	delete(root["patch"].(sourceObject), "large_text")
	records = records[:4]
	for i := 1; i < 4; i++ {
		records[i].(sourceObject)["parent"] = i - 1
	}
	value["records"] = records
	body, err = EncodeSourceJSON(value)
	require.NoError(t, err)
	_, err = ParseEnrichmentTranscript(body)
	require.ErrorIs(t, err, models.ErrEnrichmentInvalid, "nested children have an independent depth limit")
	_, err = ParseEnrichmentTranscript(bytes.Repeat([]byte{' '}, MaxEnrichmentTranscriptBytes+1))
	require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
}
