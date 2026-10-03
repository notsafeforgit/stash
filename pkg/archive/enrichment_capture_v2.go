package archive

import (
	"bytes"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

// PrepareEnrichmentRecord preserves historical v1 identities, resolves a v2
// retained prefix to an existing capture, or creates a new context-bound one.
func PrepareEnrichmentRecord(job string, work *models.EnrichmentJobArguments, transcript *EnrichmentTranscript,
	record models.EnrichmentCheckpointRecord, parent string, retained *models.SourceCapture) (*models.SourceCaptureInput, *models.SourcePostIdentifier, error) {
	if work == nil || transcript == nil || record.Ordinal < 0 || record.Ordinal >= len(transcript.Records) {
		return nil, nil, models.ErrEnrichmentInvalid
	}
	if work.Version == 1 {
		if retained != nil || record.RetainedCapture != nil {
			return nil, nil, models.ErrEnrichmentInvalid
		}
		return PrepareEnrichmentCapture(job, work, transcript, record)
	}
	if record.RetainedCapture == nil {
		if retained != nil {
			return nil, nil, models.ErrEnrichmentInvalid
		}
		return PrepareContextEnrichmentCapture(job, work, transcript, record, parent)
	}
	source := transcript.Records[record.Ordinal]
	if work.Version != 2 || work.Handoff == nil || work.CapturePolicy != CaptureContextPolicy || transcript.Schema != EnrichmentRetainedSchema ||
		!translationUUID(job) || record.JobUUID != job || !translationUUID(record.ProducerUUID) || transcript.ExtractorVersion != work.ExtractorVersion ||
		source.RetainedCapture == nil || *source.RetainedCapture != *record.RetainedCapture || retained == nil || retained.UUID != *record.RetainedCapture ||
		retained.PostUUID != work.PostUUID || retained.Origin != "legacy-enrichment" || retained.RetentionPolicy != "legacy-retained-v1" ||
		retained.Payload == nil || !retained.CapturedAt.IsZero() || retained.RecordedAt == nil || len(retained.Contexts) != 0 || parent != "" {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	digest, err := transcript.RecordDigest(record.Ordinal)
	if err != nil || digest != record.Digest {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	raw, err := RestoreCapture(retained.Payload)
	if err != nil {
		return nil, nil, err
	}
	original, err := transcript.Metadata(record.Ordinal)
	if err != nil || !bytes.Equal(raw, original) {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	post, err := ExtractCapturedPost(raw)
	if err != nil || post == nil {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	return &models.SourceCaptureInput{UUID: retained.UUID, PostUUID: retained.PostUUID, Origin: retained.Origin, Platform: retained.Platform,
		CapturedAt: retained.CapturedAt, RecordedAt: retained.RecordedAt, ExtractorVersion: retained.ExtractorVersion,
		RetentionPolicy: retained.RetentionPolicy, Metadata: retained.Metadata, Payload: *retained.Payload}, post, nil
}

// PrepareContextEnrichmentCapture builds a newly observed record only. Retained
// prefix records must resolve through the accepted original native capture;
// the checkpoint uploader is not their original observing producer.
func PrepareContextEnrichmentCapture(job string, work *models.EnrichmentJobArguments, transcript *EnrichmentTranscript,
	record models.EnrichmentCheckpointRecord, parent string) (*models.SourceCaptureInput, *models.SourcePostIdentifier, error) {
	if work == nil || work.Version != 2 || work.CapturePolicy != CaptureContextPolicy || transcript == nil ||
		!translationUUID(job) || record.JobUUID != job || !translationUUID(record.ProducerUUID) || !translationUUID(work.PostUUID) ||
		transcript.ExtractorVersion != work.ExtractorVersion || record.Ordinal < 0 || record.Ordinal >= len(transcript.Records) {
		return nil, nil, models.ErrEnrichmentInvalid
	}
	if (work.Handoff == nil && transcript.Schema != EnrichmentTranscriptSchema) || (work.Handoff != nil && transcript.Schema != EnrichmentRetainedSchema) {
		return nil, nil, models.ErrEnrichmentInvalid
	}
	source := transcript.Records[record.Ordinal]
	if source.RetainedCapture != nil || record.RetainedCapture != nil ||
		(source.Parent == nil && parent != "") || (source.Parent != nil && !translationUUID(parent)) {
		return nil, nil, models.ErrEnrichmentInvalid
	}
	digest, err := transcript.RecordDigest(record.Ordinal)
	if err != nil || digest != record.Digest {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	raw, err := transcript.Metadata(record.Ordinal)
	if err != nil {
		return nil, nil, err
	}
	post, err := ExtractCapturedPost(raw)
	if err != nil || post == nil {
		return nil, nil, models.ErrEnrichmentInvalid
	}
	metadata, err := CapturedMetadata(raw)
	if err != nil {
		return nil, nil, err
	}
	payload, err := PrepareRetainedCapture("gallery-dl", CapturedPostPlatform(*post), raw)
	if err != nil {
		return nil, nil, err
	}
	observed, err := time.Parse(time.RFC3339Nano, source.ObservedAt)
	if err != nil {
		return nil, nil, models.ErrEnrichmentInvalid
	}
	// Parent identity is evidence even when two captures contain equal text.
	id := ContextEnrichmentCaptureUUID(job, record.ProducerUUID, observed, raw, parent)
	input := &models.SourceCaptureInput{UUID: id, PostUUID: work.PostUUID, Origin: "gallery-dl", Platform: CapturedPostPlatform(*post),
		CapturedAt: observed, ExtractorVersion: &work.ExtractorVersion, RetentionPolicy: SourceRetentionVersion, Metadata: metadata, Payload: *payload}
	link, err := transcript.CaptureContext(record.Ordinal, id, parent)
	if err != nil {
		return nil, nil, err
	}
	if link != nil {
		input.RetentionPolicy, input.Contexts = CaptureContextPolicy, []models.SourceCaptureContext{*link}
		if err := CaptureContextRetention(raw, input.Contexts); err != nil {
			return nil, nil, err
		}
	}
	return input, post, nil
}

func ContextEnrichmentCaptureUUID(job, producer string, observed time.Time, raw []byte, parent string) string {
	namespace, err := uuid.Parse(job)
	if err != nil {
		return ""
	}
	key := "enrichment-capture-v2\x00" + producer + "\x00" + observed.UTC().Format(time.RFC3339Nano) + "\x00" + translationDigest(raw) + "\x00" + parent
	return uuid.NewSHA1(namespace, []byte(key)).String()
}
