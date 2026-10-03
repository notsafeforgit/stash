package scrape

import (
	"encoding/json"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// PrepareLegacyEnrichmentSeed projects accepted native evidence into a worker
// resume document. Original record slots, reasons and unscoped references remain
// in the acceptance's frozen staging body; they are not new worker observations.
func PrepareLegacyEnrichmentSeed(acceptance *models.CheckpointEvidenceAcceptance, body []byte, extractorVersion string) (json.RawMessage, error) {
	if acceptance == nil || acceptance.Version != 1 || CatalogSnapshotSHA(body) != acceptance.BodySHA256 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	staging, err := DecodeStaging(body)
	if err != nil {
		return nil, err
	}
	captures, err := PrepareStagedCaptures(acceptance.Input.SnapshotUUID, acceptance.Input.Ordinal, acceptance.Target.PostUUID, staging, acceptance.CreatedAt)
	if err != nil {
		return nil, err
	}
	if len(captures) != len(acceptance.Captures) || len(staging.Records) != acceptance.RecordCount ||
		len(staging.Pending) != acceptance.PendingCount || len(staging.Unresolved) != acceptance.UnscopedCount {
		return nil, models.ErrSourcePayloadCorrupt
	}
	records := []any{}
	indices, seen := make([]int, len(captures)), map[string]int{}
	for i, capture := range captures {
		binding := acceptance.Captures[i]
		raw, err := archive.RestoreCapture(&capture.Payload)
		if err != nil || binding.BodyIndex != i || binding.CaptureUUID != capture.UUID || CatalogSnapshotSHA(raw) != binding.PayloadSHA {
			return nil, models.ErrSourcePayloadCorrupt
		}
		if index, ok := seen[capture.UUID]; ok {
			indices[i] = index
			continue
		}
		metadata, err := archive.DecodeJSONObject(raw, archive.MaxSourcePayloadBytes)
		if err != nil {
			return nil, err
		}
		indices[i], seen[capture.UUID] = len(records), len(records)
		records = append(records, map[string]any{"kind": "context", "base": nil, "parent": nil,
			"patch": metadata, "removed": []string{}, "observed_at": nil, "retained_capture": capture.UUID})
	}
	pending := []models.EnrichmentReference{}
	seenPending := map[models.EnrichmentReference]bool{}
	for _, ref := range staging.Pending {
		if ref.Parent < 0 || ref.Parent >= len(indices) {
			return nil, models.ErrSourcePayloadCorrupt
		}
		// This code describes the new handoff's state, not an inferred historical
		// failure. Unrecognized or absent old reasons remain in frozen evidence.
		reason := "legacy_pending"
		if ref.Reason != nil && archive.EnrichmentReasonCode(*ref.Reason) {
			reason = *ref.Reason
		}
		item := models.EnrichmentReference{URL: ref.URL, Parent: indices[ref.Parent], Depth: ref.Depth, Reason: reason}
		if !seenPending[item] {
			pending = append(pending, item)
			seenPending[item] = true
		}
	}
	raw, err := archive.EncodeSourceJSON(map[string]any{"schema": archive.EnrichmentRetainedSchema, "url": acceptance.Target.URL,
		"retention_policy": archive.SourceRetentionVersion, "extractor_version": extractorVersion,
		"records": records, "pending": pending, "unresolved": []any{}})
	if err != nil {
		return nil, err
	}
	parsed, err := archive.ParseEnrichmentTranscript(raw)
	if err != nil {
		return nil, err
	}
	return parsed.Body(), nil
}
