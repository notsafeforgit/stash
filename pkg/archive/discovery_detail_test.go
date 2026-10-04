package archive

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func detailCaptureFixture(t *testing.T) (json.RawMessage, *EnrichmentTranscript, string, string, []models.EnrichmentCheckpointRecord) {
	t.Helper()
	_, body, _ := enrichmentTranscriptFixture(t)
	parsed, err := ParseEnrichmentTranscript(body)
	require.NoError(t, err)
	job, post, initialProducer, resumedProducer := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	records := []models.EnrichmentCheckpointRecord{}
	for ordinal := range parsed.Records {
		digest, err := parsed.RecordDigest(ordinal)
		require.NoError(t, err)
		record := models.EnrichmentCheckpointRecord{JobUUID: job, Ordinal: ordinal, CheckpointRevision: 1, Fence: 1, ProducerUUID: initialProducer, Digest: digest}
		if ordinal >= 3 {
			record.CheckpointRevision, record.Fence, record.ProducerUUID = 2, 2, resumedProducer
		}
		records = append(records, record)
	}
	return body, parsed, job, post, records
}

func TestDiscoveryDetailCapturesPreserveParentContextAndProducerFailover(t *testing.T) {
	body, parsed, job, post, records := detailCaptureFixture(t)
	selected := models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}
	actual, err := PrepareDiscoveryDetailCaptures(job, post, selected, parsed.URL, parsed.ExtractorVersion, body, records)
	require.NoError(t, err)
	require.Len(t, actual, len(parsed.Records))
	captures := map[string]*models.SourceCapture{}
	for ordinal, prepared := range actual {
		require.Equal(t, ordinal, prepared.Ordinal)
		require.Equal(t, post, prepared.Input.PostUUID)
		metadata, err := parsed.Metadata(ordinal)
		require.NoError(t, err)
		restored, err := RestoreCapture(&prepared.Input.Payload)
		require.NoError(t, err)
		require.Equal(t, metadata, restored)
		observed, err := time.Parse(time.RFC3339Nano, parsed.Records[ordinal].ObservedAt)
		require.NoError(t, err)
		require.Equal(t, observed.UTC(), prepared.Input.CapturedAt.UTC())
		capture := &models.SourceCapture{UUID: prepared.Input.UUID, PostUUID: post, CapturedAt: prepared.Input.CapturedAt, Payload: &prepared.Input.Payload}
		for _, link := range prepared.Input.Contexts {
			require.NoError(t, ValidateCaptureContext(link, capture, captures[link.ParentUUID]))
		}
		captures[capture.UUID] = capture
	}
	replayed, err := PrepareDiscoveryDetailCaptures(job, post, selected, parsed.URL, parsed.ExtractorVersion, body, records)
	require.NoError(t, err)
	require.Equal(t, actual, replayed)
	// Later delivery ownership does not rewrite the original observing producer.
	for i := range records {
		records[i].Fence, records[i].CheckpointRevision = 9, 9
	}
	replayed, err = PrepareDiscoveryDetailCaptures(job, post, selected, parsed.URL, parsed.ExtractorVersion, body, records)
	require.NoError(t, err)
	require.Equal(t, actual, replayed)
	last := len(records) - 1
	records[last].ProducerUUID = uuid.NewString()
	changed, err := PrepareDiscoveryDetailCaptures(job, post, selected, parsed.URL, parsed.ExtractorVersion, body, records)
	require.NoError(t, err)
	require.Equal(t, actual[:last], changed[:last])
	require.NotEqual(t, actual[last].Input.UUID, changed[last].Input.UUID)
}

func TestDiscoveryDetailCapturesRejectMissingOrChangedDeliveryEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func([]models.EnrichmentCheckpointRecord) []models.EnrichmentCheckpointRecord
	}{
		{"missing record", func(r []models.EnrichmentCheckpointRecord) []models.EnrichmentCheckpointRecord { return r[:len(r)-1] }},
		{"changed digest", func(r []models.EnrichmentCheckpointRecord) []models.EnrichmentCheckpointRecord {
			r[0].Digest = strings.Repeat("0", 64)
			return r
		}},
		{"foreign job", func(r []models.EnrichmentCheckpointRecord) []models.EnrichmentCheckpointRecord {
			r[0].JobUUID = uuid.NewString()
			return r
		}},
		{"missing producer", func(r []models.EnrichmentCheckpointRecord) []models.EnrichmentCheckpointRecord {
			r[0].ProducerUUID = ""
			return r
		}},
		{"unowned attempt", func(r []models.EnrichmentCheckpointRecord) []models.EnrichmentCheckpointRecord {
			r[0].Fence = 0
			return r
		}},
		{"missing checkpoint", func(r []models.EnrichmentCheckpointRecord) []models.EnrichmentCheckpointRecord {
			r[0].CheckpointRevision = 0
			return r
		}},
		{"record reordered", func(r []models.EnrichmentCheckpointRecord) []models.EnrichmentCheckpointRecord {
			r[0], r[1] = r[1], r[0]
			return r
		}},
		{"unobserved historical context", func(r []models.EnrichmentCheckpointRecord) []models.EnrichmentCheckpointRecord {
			id := uuid.NewString()
			r[0].RetainedCapture = &id
			return r
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, parsed, job, post, records := detailCaptureFixture(t)
			actual, err := PrepareDiscoveryDetailCaptures(job, post, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, parsed.URL, parsed.ExtractorVersion, body, test.change(records))
			require.Error(t, err)
			require.Nil(t, actual)
		})
	}
	body, parsed, job, post, records := detailCaptureFixture(t)
	initial, _, _ := enrichmentTranscriptFixture(t)
	_, err := PrepareDiscoveryDetailCaptures(job, post, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, parsed.URL, parsed.ExtractorVersion, initial, records[:3])
	require.ErrorIs(t, err, models.ErrDiscoveryConflict, "pending child work cannot appear published")
	value, err := DecodeJSONObject(body, MaxEnrichmentTranscriptBytes)
	require.NoError(t, err)
	value["records"], value["unresolved"] = []any{}, []any{}
	empty, err := EncodeSourceJSON(value)
	require.NoError(t, err)
	_, err = PrepareDiscoveryDetailCaptures(job, post, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, parsed.URL, parsed.ExtractorVersion, empty, nil)
	require.ErrorIs(t, err, models.ErrDiscoveryConflict)
}

func TestDiscoveryDetailParsingPinsExactPostAndRuntime(t *testing.T) {
	_, body, _ := enrichmentTranscriptFixture(t)
	parsed, err := ParseEnrichmentTranscript(body)
	require.NoError(t, err)
	for _, test := range []struct {
		name      string
		reference models.SourcePostIdentifier
		url       string
		runtime   string
	}{
		{"different post", models.SourcePostIdentifier{Namespace: "native:reddit", Value: "different"}, parsed.URL, parsed.ExtractorVersion},
		{"different service", models.SourcePostIdentifier{Namespace: "native:twitter", Value: "123"}, parsed.URL, parsed.ExtractorVersion},
		{"different fetch URL", models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "https://www.reddit.com/comments/different", parsed.ExtractorVersion},
		{"unsupported runtime", models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, parsed.URL, "future"},
		{"invalid ID", models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123/title"}, "https://www.reddit.com/comments/abc123/title", parsed.ExtractorVersion},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, err := ParseDiscoveryDetail(body, test.reference, test.url, test.runtime)
			require.Error(t, err)
			require.Nil(t, actual)
		})
	}
	retained, err := EncodeSourceJSON(retainedTranscriptFixture(t))
	require.NoError(t, err)
	actual, err := ParseDiscoveryDetail(retained, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, parsed.URL, "current-runtime")
	require.Error(t, err)
	require.Nil(t, actual)
}
