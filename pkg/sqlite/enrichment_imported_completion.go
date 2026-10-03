package sqlite

import (
	"context"
	"time"

	"github.com/google/uuid"
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

// Only the frozen importer may finish a newly created or import-owned hold
// using old proof. Public completion continues to require actual scoped captures.
func completeImportedEnrichment(ctx context.Context, target *models.EnrichmentTarget, receipt, capture *string, now time.Time) (*models.EnrichmentTarget, error) {
	if target.State != "held" || target.Revision < 1 || target.Origin != "migration" || target.CollectionRevision != 1 ||
		!validJobTime(now) || now.Before(target.UpdatedAt) || (receipt == nil) == (capture == nil) {
		return nil, models.ErrAutomationSnapshotConflict
	}
	proof, basis := receipt, "legacy_receipt"
	if proof == nil {
		proof, basis = capture, "legacy_capture"
	}
	if !validSourceRunUUID(*proof) {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	id := uuid.NewSHA1(uuid.MustParse(target.UUID), []byte(basis+"\x00"+*proof)).String()
	digest, err := sourceSignature("stash-imported-enrichment-completion-v1", importedEnrichmentProof{id, target.UUID, target.Revision, receipt, capture})
	if err != nil {
		return nil, err
	}
	complete := enrichmentAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, `INSERT INTO enrichment_completions(uuid,target_uuid,expected_revision,request_digest,capture_count,legacy_receipt_uuid,legacy_capture_uuid,created_at)
 VALUES(?,?,?,?,0,?,?,?)`, id, target.UUID, target.Revision, digest, receipt, capture, now.UTC())
	if err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, "UPDATE enrichment_targets SET state='completed',completion_uuid=?,reason='',revision=revision+1,updated_at=? WHERE uuid=?", id, now.UTC(), target.UUID)
	if err != nil {
		return nil, err
	}
	ret, err := (&EnrichmentWorkStore{}).Target(ctx, target.UUID)
	*complete = err == nil
	return ret, err
}
