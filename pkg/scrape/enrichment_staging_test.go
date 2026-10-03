package scrape_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func restoreStagedBody(t *testing.T, staged *scrape.EnrichmentStaging, index int) map[string]any {
	t.Helper()
	item := staged.Bodies[index]
	out := map[string]any{}
	if item.Base != nil {
		require.Less(t, *item.Base, index)
		out = restoreStagedFlat(t, staged, *item.Base)
	}
	for _, key := range item.Removed {
		delete(out, key)
	}
	for key, value := range item.Patch {
		out[key] = value
	}
	for key, parent := range item.Parents {
		require.Less(t, parent, index)
		out[key] = restoreStagedBody(t, staged, parent)
	}
	return out
}

func restoreStagedFlat(t *testing.T, staged *scrape.EnrichmentStaging, index int) map[string]any {
	out := restoreStagedBody(t, staged, index)
	for key := range staged.Bodies[index].Parents {
		delete(out, key)
	}
	return out
}

func TestLegacyStagingSharesBodiesAndPreservesAllSourceSlots(t *testing.T) {
	parent := map[string]any{"category": "reddit", "id": "album", "title": strings.Repeat("Shared caption ", 40), "author": "juniper",
		"media_metadata": map[string]any{"a": map[string]any{"p": []any{"retained historical preview"}}}, "source_extractor_url": "https://www.reddit.com/comments/album"}
	child := map[string]any{"category": "redgifs", "id": json.Number("9007199254740993"), "num": 1, "_reddit": parent}
	input := map[string]any{"records": []any{map[string]any{"kind": "post", "metadata": parent}, map[string]any{"kind": "media", "metadata": parent},
		map[string]any{"kind": "media", "metadata": child}, map[string]any{"kind": "media", "metadata": child}},
		"extractor_version": "1.32.15-dev", "retry_reason": "rate_limited",
		"pending_children": []any{map[string]any{"url": "https://redgifs.com/watch/pending", "parent": parent, "depth": 1, "reason": "rate_limited"}},
		"unresolved":       []any{map[string]any{"url": "https://outside.invalid/item", "reason": "external_reference_only"}}}
	raw, err := archive.EncodeSourceJSON(input)
	require.NoError(t, err)
	before := append([]byte(nil), raw...)
	staged, body, reason := scrape.ConvertEnrichmentStaging(raw)
	require.Empty(t, reason)
	require.Equal(t, before, []byte(raw))
	require.Equal(t, "unrecorded", staged.ObservationTimeBasis)
	require.Equal(t, "1.32.15-dev", *staged.ExtractorVersion)
	require.Len(t, staged.Bodies, 2)
	require.Len(t, staged.Records, 4)
	require.Equal(t, []int{0, 0, 1, 1}, []int{staged.Records[0].Body, staged.Records[1].Body, staged.Records[2].Body, staged.Records[3].Body})
	require.Equal(t, 0, staged.Pending[0].Parent)
	for i, record := range input["records"].([]any) {
		expected, err := archive.EncodeSourceJSON(record.(map[string]any)["metadata"])
		require.NoError(t, err)
		restored, err := archive.EncodeSourceJSON(restoreStagedBody(t, staged, staged.Records[i].Body))
		require.NoError(t, err)
		require.Equal(t, string(expected), string(restored))
	}
	require.NotContains(t, string(body), "observed_at")
	require.NotContains(t, string(body), "producer_uuid")
	require.NotContains(t, string(body), "captured_at")
	_, err = archive.ParseEnrichmentTranscript(body)
	require.Error(t, err, "migration evidence cannot impersonate a native producer checkpoint")
	_, again, reason := scrape.ConvertEnrichmentStaging(append([]byte(" \n"), raw...))
	require.Empty(t, reason)
	require.Equal(t, body, again)
}

func TestLegacyStagingDeltasPreserveNullRemovedFieldsAndTwoParents(t *testing.T) {
	text := strings.Repeat("Useful shared data ", 30)
	raw := `{"records":[{"kind":"post","metadata":{"caption":` + stagingJSONString(t, text) + `,"removed":"old","nullable":"old"}},` +
		`{"kind":"media","metadata":{"caption":` + stagingJSONString(t, text) + `,"nullable":null}},` +
		`{"kind":"media","metadata":{"_parent":{"id":"one"},"_reddit":{"id":"two"},"id":"child"}}]}`
	staged, _, reason := scrape.ConvertEnrichmentStaging([]byte(raw))
	require.Empty(t, reason)
	require.NotNil(t, staged.Bodies[1].Base)
	require.Equal(t, []string{"removed"}, staged.Bodies[1].Removed)
	require.Equal(t, map[string]any{"caption": text, "nullable": nil}, restoreStagedBody(t, staged, 1))
	require.Equal(t, map[string]any{"_parent": map[string]any{"id": "one"}, "_reddit": map[string]any{"id": "two"}, "id": "child"}, restoreStagedBody(t, staged, staged.Records[2].Body))
	require.Nil(t, staged.ExtractorVersion)
}

func stagingJSONString(t *testing.T, value string) string {
	t.Helper()
	body, err := json.Marshal(value)
	require.NoError(t, err)
	return string(body)
}

func TestLegacyStagingRejectsUnknownAndMalformedFormatsWithoutPartialProjection(t *testing.T) {
	for _, raw := range []string{
		`{"records":[],"records":[]}`, `[]`, `{"records":[]}`, `{"records":[{"kind":"post","metadata":{}}],"future":true}`,
		`{"records":[{"kind":"post","metadata":{},"observed_at":"2026-09-29T00:00:00Z"}]}`,
		`{"records":[{"kind":"unknown","metadata":{}}]}`, `{"records":[{"kind":"post","metadata":{"_reddit":null}}]}`,
		`{"records":[{"kind":"post","metadata":{}}],"pending_children":[{"url":"https://example.invalid","parent":{},"depth":3}]}`,
		`{"records":[{"kind":"post","metadata":{}}],"unresolved":[{"url":"https://example.invalid","parent":{}}]}`,
		`{"records":[{"kind":"post","metadata":{"_parent":{"_parent":{"_parent":{}}}}}]}`,
		`{"records":[{"kind":"post","metadata":{}}],"extractor_version":false}`,
		strings.Repeat(" ", scrape.CatalogChunkLimit+1),
	} {
		staged, body, reason := scrape.ConvertEnrichmentStaging([]byte(raw))
		require.NotEmpty(t, reason)
		require.Nil(t, staged)
		require.Nil(t, body)
	}
}

func TestLegacyStagingRetainsUnscopedReferencesAndMaximumRecordCount(t *testing.T) {
	records := make([]any, 256)
	for i := range records {
		records[i] = map[string]any{"kind": "media", "metadata": map[string]any{"id": "same"}}
	}
	input := map[string]any{"records": records, "unresolved": []any{map[string]any{"url": "old-unsupported-reference"}},
		"pending_children": []any{map[string]any{"url": "https://imgur.com/example", "parent": map[string]any{}, "depth": 1}}}
	raw, err := archive.EncodeSourceJSON(input)
	require.NoError(t, err)
	staged, _, reason := scrape.ConvertEnrichmentStaging(raw)
	require.Empty(t, reason)
	require.Len(t, staged.Records, 256)
	require.Len(t, staged.Bodies, 2)
	require.Nil(t, staged.Unresolved[0].Reason)
	require.Nil(t, staged.Pending[0].Reason)
	input["records"] = append(records, records[0])
	raw, err = archive.EncodeSourceJSON(input)
	require.NoError(t, err)
	_, _, reason = scrape.ConvertEnrichmentStaging(raw)
	require.Equal(t, "invalid_legacy_checkpoint_records", reason)
}
