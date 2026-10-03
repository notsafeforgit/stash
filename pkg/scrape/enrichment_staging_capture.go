package scrape

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// PrepareStagedCaptures retains the original legacy bodies as undated native
// evidence. It neither applies today's retention policy nor invents an observing
// producer. Every original record and pending parent keeps its body index.
func PrepareStagedCaptures(snapshot string, ordinal int64, post string, staging *EnrichmentStaging, recorded time.Time) ([]models.SourceCaptureInput, error) {
	namespace, err := uuid.Parse(snapshot)
	if err != nil || namespace == uuid.Nil || ordinal < 1 || staging == nil || staging.Format != EnrichmentStagingPolicy ||
		staging.ObservationTimeBasis != "unrecorded" || recorded.IsZero() {
		return nil, models.ErrEnrichmentInvalid
	}
	postID, err := uuid.Parse(post)
	if err != nil || postID == uuid.Nil || len(staging.Bodies) == 0 || len(staging.Bodies) > 4096 {
		return nil, models.ErrEnrichmentInvalid
	}
	flat, expanded := []map[string]any{}, []map[string]any{}
	result := make([]models.SourceCaptureInput, 0, len(staging.Bodies))
	expandedBytes := 0
	for index, body := range staging.Bodies {
		data := map[string]any{}
		if body.Base != nil {
			if *body.Base < 0 || *body.Base >= index {
				return nil, models.ErrEnrichmentInvalid
			}
			for key, value := range flat[*body.Base] {
				data[key] = value
			}
		}
		for _, key := range body.Removed {
			if _, ok := data[key]; !ok {
				return nil, models.ErrEnrichmentInvalid
			}
			delete(data, key)
		}
		for key, value := range body.Patch {
			data[key] = value
		}
		flat = append(flat, data)
		full := map[string]any{}
		for key, value := range data {
			full[key] = value
		}
		for key, parent := range body.Parents {
			if (key != "_parent" && key != "_reddit") || parent < 0 || parent >= index {
				return nil, models.ErrEnrichmentInvalid
			}
			if _, exists := full[key]; exists {
				return nil, models.ErrEnrichmentInvalid
			}
			full[key] = expanded[parent]
		}
		raw, err := archive.EncodeSourceJSON(full)
		if err != nil || len(raw) > archive.MaxSourcePayloadBytes || len(raw) > archive.MaxEnrichmentExpandedBytes-expandedBytes {
			return nil, models.ErrEnrichmentInvalid
		}
		expandedBytes += len(raw)
		reference, err := archive.ExtractCapturedPost(raw)
		if err != nil || reference == nil {
			return nil, fmt.Errorf("legacy checkpoint body %d has no supported post identity: %w", index, models.ErrEnrichmentInvalid)
		}
		metadata, err := archive.CapturedMetadata(raw)
		if err != nil {
			return nil, err
		}
		platform := archive.CapturedPostPlatform(*reference)
		payload, err := archive.PrepareRetainedCapture("gallery-dl", platform, raw)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("legacy-enrichment-capture-v1\x00%d\x00%s\x00%s", ordinal, post, CatalogSnapshotSHA(raw))
		stamp := recorded.UTC()
		result = append(result, models.SourceCaptureInput{UUID: uuid.NewSHA1(namespace, []byte(key)).String(), PostUUID: post,
			Origin: "legacy-enrichment", Platform: platform, RecordedAt: &stamp, ExtractorVersion: staging.ExtractorVersion,
			RetentionPolicy: "legacy-retained-v1", Metadata: metadata, Payload: *payload})
		expanded = append(expanded, full)
	}
	return result, nil
}

// DecodeStaging uses exact JSON numbers; decoding map values through float64
// would alter opaque service IDs before they reached native capture storage.
func DecodeStaging(raw []byte) (*EnrichmentStaging, error) {
	value, err := archive.DecodeJSONObject(raw, MaxEnrichmentStagingBytes)
	if err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	encoded, err := archive.EncodeSourceJSON(value)
	if err != nil {
		return nil, err
	}
	var result EnrichmentStaging
	// The projection contains map values: preserve number spelling on decode.
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	return &result, nil
}
