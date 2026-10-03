package archive

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

// CapturedMetadata uses source text verbatim. It follows the download producer's
// projection; a linked media host cannot replace its enclosing Reddit caption.
func CapturedMetadata(raw []byte) (models.SourcePostMetadata, error) {
	ret := models.SourcePostMetadata{}
	data, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return ret, err
	}
	if parent, ok := data["_reddit"].(sourceObject); ok && sourceTruthy(parent["id"]) {
		data = parent
	}
	for _, field := range []struct {
		out  **string
		keys []string
	}{
		{&ret.Title, []string{"title"}},
		{&ret.OriginalText, []string{"content", "selftext", "title"}},
		{&ret.PublishedAt, []string{"date"}},
		{&ret.Language, []string{"lang", "language"}},
	} {
		for _, key := range field.keys {
			if value, ok := data[key].(string); ok && value != "" {
				*field.out = &value
				break
			}
		}
	}
	if ret.PublishedAt != nil {
		basis := "source"
		ret.DateBasis = &basis
	}
	encoded, err := json.Marshal(ret)
	if err != nil || len(encoded) > 262144 {
		return models.SourcePostMetadata{}, models.ErrEnrichmentInvalid
	}
	return ret, nil
}

// PrepareEnrichmentCapture binds retained bytes and their original observing
// producer to a portable capture. Equal records from the same observation share
// a capture even when the transcript also needs a separate context record.
func PrepareEnrichmentCapture(job string, work *models.EnrichmentJobArguments, transcript *EnrichmentTranscript, record models.EnrichmentCheckpointRecord) (*models.SourceCaptureInput, *models.SourcePostIdentifier, error) {
	if work == nil || transcript == nil || !translationUUID(job) || record.JobUUID != job || !translationUUID(record.ProducerUUID) ||
		!translationUUID(work.PostUUID) || transcript.ExtractorVersion != work.ExtractorVersion {
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
	payload, err := PrepareRetainedCapture("gallery-dl", strings.TrimPrefix(post.Namespace, "native:"), raw)
	if err != nil {
		return nil, nil, err
	}
	observed, err := time.Parse(time.RFC3339Nano, transcript.Records[record.Ordinal].ObservedAt)
	if err != nil {
		return nil, nil, models.ErrEnrichmentInvalid
	}
	key := "enrichment-capture-v1\x00" + record.ProducerUUID + "\x00" + observed.UTC().Format(time.RFC3339Nano) + "\x00" + translationDigest(raw)
	id := uuid.NewSHA1(uuid.MustParse(job), []byte(key)).String()
	return &models.SourceCaptureInput{UUID: id, PostUUID: work.PostUUID, Origin: "gallery-dl", Platform: strings.TrimPrefix(post.Namespace, "native:"),
		CapturedAt: observed, ExtractorVersion: &work.ExtractorVersion, RetentionPolicy: transcript.RetentionPolicy, Metadata: metadata, Payload: *payload}, post, nil
}

func EnrichmentCompletionUUID(job string) (string, error) {
	if !translationUUID(job) {
		return "", models.ErrEnrichmentInvalid
	}
	return uuid.NewSHA1(uuid.MustParse(job), []byte("enrichment-completion-v1")).String(), nil
}

// The job result is a small projection of domain receipts, never another copy
// of captures or a caller-provided success flag.
func EnrichmentPublicationResult(value models.EnrichmentPublication) (json.RawMessage, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	tree, err := DecodeJSONObject(body, 16384)
	if err != nil {
		return nil, err
	}
	return EncodeSourceJSON(tree)
}
