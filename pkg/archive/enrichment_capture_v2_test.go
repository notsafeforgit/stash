package archive

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestContextEnrichmentCaptureDistinguishesOriginalEvidenceFromDelivery(t *testing.T) {
	value := retainedTranscriptFixture(t)
	appendRetainedChild(value)
	body, err := EncodeSourceJSON(value)
	require.NoError(t, err)
	transcript, err := ParseEnrichmentTranscript(body)
	require.NoError(t, err)
	job, producer := uuid.NewString(), uuid.NewString()
	work := &models.EnrichmentJobArguments{Version: 2, PostUUID: uuid.NewString(), ExtractorVersion: "current-runtime", CapturePolicy: CaptureContextPolicy,
		Handoff: &models.EnrichmentJobHandoff{UUID: uuid.NewString(), PlanSHA256: strings.Repeat("a", 64), SeedSHA256: strings.Repeat("b", 64)}}
	raw, err := transcript.Metadata(0)
	require.NoError(t, err)
	payload, err := PrepareRetainedCapture("legacy-enrichment", "reddit", raw)
	require.NoError(t, err)
	metadata, err := CapturedMetadata(raw)
	require.NoError(t, err)
	received := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	original := &models.SourceCapture{UUID: *transcript.Records[0].RetainedCapture, PostUUID: work.PostUUID, Origin: "legacy-enrichment", Platform: "reddit",
		RecordedAt: &received, RetentionPolicy: "legacy-retained-v1", Payload: payload, Metadata: metadata}
	digest, err := transcript.RecordDigest(0)
	require.NoError(t, err)
	record := models.EnrichmentCheckpointRecord{JobUUID: job, Ordinal: 0, Digest: digest, ProducerUUID: producer, RetainedCapture: &original.UUID}
	retained, _, err := PrepareEnrichmentRecord(job, work, transcript, record, "", original)
	require.NoError(t, err)
	require.Equal(t, original.UUID, retained.UUID)
	require.True(t, retained.CapturedAt.IsZero())
	require.Equal(t, received, *retained.RecordedAt)
	require.Nil(t, retained.ExtractorVersion)
	digest, err = transcript.RecordDigest(1)
	require.NoError(t, err)
	record.Ordinal, record.Digest, record.RetainedCapture = 1, digest, nil
	child, _, err := PrepareEnrichmentRecord(job, work, transcript, record, original.UUID, nil)
	require.NoError(t, err)
	require.Equal(t, "2026-10-03T12:00:00Z", child.CapturedAt.Format(time.RFC3339))
	require.Nil(t, child.RecordedAt)
	require.Equal(t, work.ExtractorVersion, *child.ExtractorVersion)
	require.Equal(t, []models.SourceCaptureContext{{CaptureUUID: child.UUID, Path: "/_reddit", ParentUUID: original.UUID}}, child.Contexts)
	require.Equal(t, CaptureContextPolicy, child.RetentionPolicy)
	restored, err := RestoreCapture(&child.Payload)
	require.NoError(t, err)
	require.Contains(t, string(restored), "historical preview", "old context is retained without applying the fresh child's reduction policy to it")
	other, _, err := PrepareEnrichmentRecord(job, work, transcript, record, uuid.NewString(), nil)
	require.NoError(t, err)
	require.NotEqual(t, child.UUID, other.UUID, "equal source text with a different original parent is different evidence")
	_, _, err = PrepareEnrichmentRecord(job, work, transcript, record, "", nil)
	require.Error(t, err)
}
