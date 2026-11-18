package sqlite

import (
	"github.com/stashapp/stash/pkg/models"
)

type importedEnrichmentProof struct {
	UUID             string  `json:"uuid"`
	TargetUUID       string  `json:"target_uuid"`
	ExpectedRevision int     `json:"expected_revision"`
	ReceiptUUID      *string `json:"receipt_uuid"`
	CaptureUUID      *string `json:"capture_uuid"`
}

func (row enrichmentCompletionRow) resolveImported(captures []string) (*models.EnrichmentCompletion, error) {
	if (row.LegacyReceiptUUID == nil) == (row.LegacyCaptureUUID == nil) || row.CaptureCount != 0 || len(captures) != 0 ||
		row.ExpectedRevision < 1 || !validJobTime(row.CreatedAt) || !validSourceRunUUID(row.UUID) || !validSourceRunUUID(row.TargetUUID) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	proof, basis := row.LegacyReceiptUUID, "legacy_receipt"
	if proof == nil {
		proof, basis = row.LegacyCaptureUUID, "legacy_capture"
	}
	if !validSourceRunUUID(*proof) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	digest, err := sourceSignature("stash-imported-enrichment-completion-v1", importedEnrichmentProof{
		row.UUID, row.TargetUUID, row.ExpectedRevision, row.LegacyReceiptUUID, row.LegacyCaptureUUID})
	if err != nil || digest != row.Digest {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.EnrichmentCompletion{EnrichmentCompletionInput: models.EnrichmentCompletionInput{
		UUID: row.UUID, TargetUUID: row.TargetUUID, ExpectedRevision: row.ExpectedRevision, CaptureUUIDs: []string{}},
		Basis: basis, LegacyReceiptUUID: row.LegacyReceiptUUID, LegacyCaptureUUID: row.LegacyCaptureUUID, CreatedAt: row.CreatedAt}, nil
}
