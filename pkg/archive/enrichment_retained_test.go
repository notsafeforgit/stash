package archive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func retainedTranscriptFixture(t *testing.T) sourceObject {
	t.Helper()
	return sourceObject{"schema": EnrichmentRetainedSchema, "url": "https://www.reddit.com/comments/retained",
		"retention_policy": SourceRetentionVersion, "extractor_version": "current-runtime",
		"records": []any{sourceObject{"kind": "context", "base": nil, "parent": nil, "observed_at": nil,
			"retained_capture": uuid.NewString(), "removed": []any{}, "patch": sourceObject{
				"category": "reddit", "id": "retained", "title": "Original caption", "author": sourceObject{"id": json.Number("9007199254740993")},
				"media_metadata": sourceObject{"one": sourceObject{"p": []any{"historical preview"}}}}}},
		"pending":    []any{sourceObject{"url": "https://redgifs.com/watch/pending", "parent": 0, "depth": 1, "reason": "rate_limited"}},
		"unresolved": []any{}}
}

func appendRetainedChild(value sourceObject) {
	value["records"] = append(value["records"].([]any), sourceObject{"kind": "media", "base": nil, "parent": 0, "removed": []any{},
		"observed_at": "2026-10-03T12:00:00Z", "patch": sourceObject{"category": "redgifs", "id": "pending",
			"source_extractor_url": "https://redgifs.com/watch/pending", "_url": "https://media.invalid/full.mp4"}})
	value["pending"] = []any{}
}

func TestRetainedTranscriptPreservesOldEvidenceAndNewChildObservation(t *testing.T) {
	value := retainedTranscriptFixture(t)
	body, err := EncodeSourceJSON(value)
	require.NoError(t, err)
	initial, err := ParseEnrichmentTranscript(body)
	require.NoError(t, err)
	require.Empty(t, initial.Records[0].ObservedAt)
	require.NotNil(t, initial.Records[0].RetainedCapture)
	old, err := initial.Metadata(0)
	require.NoError(t, err)
	require.Contains(t, string(old), "9007199254740993")
	require.Contains(t, string(old), "historical preview")
	require.NotContains(t, string(old), "source_extractor_url")
	appendRetainedChild(value)
	body, err = EncodeSourceJSON(value)
	require.NoError(t, err)
	next, err := ParseEnrichmentTranscript(body)
	require.NoError(t, err)
	require.True(t, next.Extends(initial))
	require.Nil(t, next.Records[1].RetainedCapture)
	require.Equal(t, "2026-10-03T12:00:00Z", next.Records[1].ObservedAt)
	child, err := next.Metadata(1)
	require.NoError(t, err)
	restored, err := DecodeJSONObject(child, MaxSourcePayloadBytes)
	require.NoError(t, err)
	parent, err := EncodeSourceJSON(restored["_reddit"])
	require.NoError(t, err)
	require.Equal(t, old, parent)
	for i, record := range next.Records {
		encoded, err := EncodeSourceJSON(record)
		require.NoError(t, err)
		value, err := DecodeJSONObject(encoded, MaxSourcePayloadBytes)
		require.NoError(t, err)
		canonical, err := EncodeSourceJSON(value)
		require.NoError(t, err)
		digest, err := next.RecordDigest(i)
		require.NoError(t, err)
		require.Equal(t, translationDigest(canonical), digest)
	}
	encoded, err := json.Marshal(next)
	require.NoError(t, err)
	roundtrip, err := ParseEnrichmentTranscript(encoded)
	require.NoError(t, err)
	require.Equal(t, next.Body(), roundtrip.Body())
}

func TestRetainedTranscriptRejectsInventedObservationAndRewrittenContext(t *testing.T) {
	for name, alter := range map[string]func(sourceObject){
		"legacy transcript":                  func(v sourceObject) { v["schema"] = EnrichmentTranscriptSchema },
		"fake observation time":              func(v sourceObject) { v["records"].([]any)[0].(sourceObject)["observed_at"] = "2026-10-03T12:00:00Z" },
		"invalid capture UUID":               func(v sourceObject) { v["records"].([]any)[0].(sourceObject)["retained_capture"] = "not-a-capture" },
		"null capture UUID":                  func(v sourceObject) { v["records"].([]any)[0].(sourceObject)["retained_capture"] = nil },
		"retained media claims":              func(v sourceObject) { v["records"].([]any)[0].(sourceObject)["kind"] = "media" },
		"duplicate capture":                  func(v sourceObject) { v["records"] = append(v["records"].([]any), v["records"].([]any)[0]) },
		"new root":                           func(v sourceObject) { appendRetainedChild(v); v["records"].([]any)[1].(sourceObject)["parent"] = nil },
		"new delta copies unobserved fields": func(v sourceObject) { appendRetainedChild(v); v["records"].([]any)[1].(sourceObject)["base"] = 0 },
		"missing new observation time": func(v sourceObject) {
			appendRetainedChild(v)
			v["records"].([]any)[1].(sourceObject)["observed_at"] = nil
		},
		"retained after new observation": func(v sourceObject) {
			appendRetainedChild(v)
			v["records"] = append(v["records"].([]any), v["records"].([]any)[0])
		},
		"new unretained payload": func(v sourceObject) {
			appendRetainedChild(v)
			v["records"].([]any)[1].(sourceObject)["patch"].(sourceObject)["cookies"] = "fixture-private-value"
		},
		"unbound inline parent": func(v sourceObject) {
			appendRetainedChild(v)
			v["records"].([]any)[1].(sourceObject)["patch"].(sourceObject)["_parent"] = sourceObject{"category": "reddit", "id": "unreviewed"}
		},
		"oversized retained payload": func(v sourceObject) {
			v["records"].([]any)[0].(sourceObject)["patch"].(sourceObject)["title"] = strings.Repeat("x", MaxSourcePayloadBytes)
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := retainedTranscriptFixture(t)
			alter(value)
			body, err := EncodeSourceJSON(value)
			require.NoError(t, err)
			_, err = ParseEnrichmentTranscript(body)
			require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
		})
	}
}

func TestRetainedTranscriptCannotAddUnreviewedHistoricalCapturesOnRetry(t *testing.T) {
	value := retainedTranscriptFixture(t)
	body, err := EncodeSourceJSON(value)
	require.NoError(t, err)
	before, err := ParseEnrichmentTranscript(body)
	require.NoError(t, err)
	additional := sourceObject{}
	for key, data := range value["records"].([]any)[0].(sourceObject) {
		additional[key] = data
	}
	additional["retained_capture"] = uuid.NewString()
	value["records"] = append(value["records"].([]any), additional)
	body, err = EncodeSourceJSON(value)
	require.NoError(t, err)
	after, err := ParseEnrichmentTranscript(body)
	require.NoError(t, err)
	require.False(t, after.Extends(before))
}
