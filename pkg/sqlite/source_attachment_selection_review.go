package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

func validateAttachmentSelectionReview(input models.AttachmentSelectionReviewInput) error {
	id, err := archiveUUID(input.PostUUID)
	if err != nil || id != input.PostUUID || input.PostRevision < 1 || !validAccountText(input.Reason, 4096, true) {
		return models.ErrAttachmentSelectionReviewInvalid
	}
	switch input.Mode {
	case "automatic", "pinned":
		id, err := archiveUUID(input.CaptureUUID)
		if err != nil || id != input.CaptureUUID {
			return models.ErrAttachmentSelectionReviewInvalid
		}
	case "disabled":
		if input.CaptureUUID != "" {
			return models.ErrAttachmentSelectionReviewInvalid
		}
	default:
		return models.ErrAttachmentSelectionReviewInvalid
	}
	return nil
}

func attachmentSelectionReviewChoice(input models.AttachmentSelectionReviewInput) models.AttachmentSelectionInput {
	return models.AttachmentSelectionInput{PostUUID: input.PostUUID, ExpectedPostRevision: input.PostRevision,
		Mode: input.Mode, CaptureUUID: input.CaptureUUID, Origin: "review", Reason: input.Reason}
}

func attachmentSelectionReviewList(selected *models.AttachmentSelection) *models.AttachmentSelectionReviewList {
	if selected == nil {
		return nil
	}
	d := selected.Decision
	ret := &models.AttachmentSelectionReviewList{DecisionUUID: d.UUID, Revision: d.Revision, Mode: d.Mode,
		Origin: d.Origin, Reason: d.Reason, CaptureUUID: d.CaptureUUID,
		ManifestUUIDs: append([]string{}, d.ManifestUUIDs...), Complete: selected.Complete,
		DeclaredAlbum: selected.DeclaredAlbum, ExpectedCount: selected.ExpectedCount,
		Entries: make([]models.AttachmentSelectionReviewEntry, 0, len(selected.Entries))}
	for _, entry := range selected.Entries {
		ret.Entries = append(ret.Entries, models.AttachmentSelectionReviewEntry{Position: entry.Position,
			AttachmentUUID: entry.Attachment.UUID, Reference: models.SourcePostIdentifierSummary{
				Namespace: entry.Attachment.Reference.Namespace, Value: entry.Attachment.Reference.Value}, MediaKind: entry.MediaKind})
	}
	return ret
}

const selectionReviewManifestsQuery = `SELECT m.*,
 (SELECT c.capture_uuid FROM source_capture_attachment_manifests c
  WHERE c.manifest_uuid=m.uuid ORDER BY c.capture_uuid LIMIT 1) AS capture_uuid
 FROM source_attachment_manifests m WHERE m.post_uuid=? AND m.uuid>? ORDER BY m.uuid LIMIT ?`

func (s *SourceAttachmentStore) ReviewSelectionManifests(ctx context.Context, post, after string, limit int) ([]models.AttachmentSelectionReviewManifest, error) {
	if id, err := archiveUUID(post); err != nil || id != post || limit < 1 || limit > 100 {
		return nil, models.ErrAttachmentSelectionReviewInvalid
	}
	if after != "" {
		if id, err := archiveUUID(after); err != nil || id != after {
			return nil, models.ErrAttachmentSelectionReviewInvalid
		}
	}
	var rows []struct {
		attachmentManifestRow
		CaptureUUID string `db:"capture_uuid"`
	}
	// Both manifest pagination and the one witness lookup use their scoped
	// indexes. Do not page through every capture or reconstruct source payloads.
	if err := dbWrapper.Select(ctx, &rows, selectionReviewManifestsQuery, post, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.AttachmentSelectionReviewManifest, 0, len(rows))
	for _, row := range rows {
		manifest := row.resolve()
		if !archive.ValidSHA256(manifest.Signature) || manifest.Version != archive.AttachmentManifestVersion {
			return nil, models.ErrSourcePayloadCorrupt
		}
		ret = append(ret, models.AttachmentSelectionReviewManifest{UUID: manifest.UUID, CaptureUUID: row.CaptureUUID,
			Complete: manifest.Complete, DeclaredAlbum: manifest.DeclaredAlbum, ExpectedCount: manifest.ExpectedCount, EntryCount: manifest.EntryCount})
	}
	return ret, nil
}

func attachmentSelectionReviewDigest(input models.AttachmentSelectionReviewInput, previous, selected string) (string, error) {
	return sourceSignature("stash-attachment-selection-review-v1", struct {
		Input             models.AttachmentSelectionReviewInput `json:"input"`
		PreviousDecision  string                                `json:"previous_decision"`
		SelectedSignature string                                `json:"selected_signature"`
	}{input, previous, selected})
}

func (s *SourceAttachmentStore) PreviewSelectionReview(ctx context.Context, input models.AttachmentSelectionReviewInput) (*models.AttachmentSelectionReviewPreview, error) {
	if err := validateAttachmentSelectionReview(input); err != nil {
		return nil, err
	}
	_, selected, _, err := s.prepareSelectionChoice(ctx, attachmentSelectionReviewChoice(input))
	if err != nil {
		return nil, err
	}
	current, err := s.Selection(ctx, input.PostUUID)
	if err != nil {
		return nil, err
	}
	signature, err := selectedAttachmentSignature(selected)
	if err != nil {
		return nil, err
	}
	ret := &models.AttachmentSelectionReviewPreview{Input: input, Current: attachmentSelectionReviewList(current),
		Proposed: *attachmentSelectionReviewList(selected), Changed: true}
	previous := ""
	if current != nil {
		previous = current.Decision.UUID
		currentSignature, err := selectedAttachmentSignature(current)
		if err != nil {
			return nil, err
		}
		capture := ""
		if current.Decision.CaptureUUID != nil {
			capture = *current.Decision.CaptureUUID
		}
		ret.Changed = signature != currentSignature || current.Decision.Origin != "review" ||
			current.Decision.Reason != input.Reason || capture != input.CaptureUUID
	}
	ret.Digest, err = attachmentSelectionReviewDigest(input, previous, signature)
	return ret, err
}

func attachmentSelectionReviewRequest(input models.AttachmentSelectionReviewApplyInput) ([]byte, error) {
	if err := validateAttachmentSelectionReview(input.AttachmentSelectionReviewInput); err != nil {
		return nil, err
	}
	id, err := archiveUUID(input.RequestUUID)
	if err != nil || id != input.RequestUUID || !archive.ValidSHA256(input.Digest) {
		return nil, models.ErrAttachmentSelectionReviewInvalid
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > 16384 {
		return nil, models.ErrAttachmentSelectionReviewInvalid
	}
	return encoded, nil
}

func attachmentSelectionReviewSignature(input models.AttachmentSelectionReviewApplyInput, decision string) (string, error) {
	return sourceSignature("stash-attachment-selection-receipt-v1", struct {
		Request      models.AttachmentSelectionReviewApplyInput `json:"request"`
		DecisionUUID string                                     `json:"decision_uuid"`
	}{input, decision})
}

func (s *SourceAttachmentStore) ApplySelectionReview(ctx context.Context, input models.AttachmentSelectionReviewApplyInput) (*models.AttachmentSelectionReview, bool, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, false, err
	}
	encoded, err := attachmentSelectionReviewRequest(input)
	if err != nil {
		return nil, false, err
	}
	prior, err := s.SelectionReview(ctx, input.RequestUUID)
	if err != nil {
		return nil, false, err
	}
	if prior != nil {
		previous, err := attachmentSelectionReviewRequest(prior.Request)
		if err != nil {
			return nil, false, err
		}
		if string(previous) != string(encoded) {
			return nil, false, models.ErrAttachmentSelectionReviewReplay
		}
		return prior, true, nil
	}
	preview, err := s.PreviewSelectionReview(ctx, input.AttachmentSelectionReviewInput)
	if err != nil {
		return nil, false, err
	}
	if preview.Digest != input.Digest {
		return nil, false, models.ErrAttachmentSelectionConflict
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrAttachmentSelectionReviewInvalid
		}
		return nil
	})
	selected, err := s.DecideSelection(ctx, attachmentSelectionReviewChoice(input.AttachmentSelectionReviewInput))
	if err != nil {
		return nil, false, err
	}
	signature, err := attachmentSelectionReviewSignature(input, selected.Decision.UUID)
	if err != nil {
		return nil, false, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO attachment_selection_reviews(request_uuid,post_uuid,decision_uuid,request_json,signature)
 VALUES(?,?,?,?,?)`, input.RequestUUID, input.PostUUID, selected.Decision.UUID, string(encoded), signature); err != nil {
		return nil, false, err
	}
	ret, err := s.SelectionReview(ctx, input.RequestUUID)
	if err == nil && ret == nil {
		err = models.ErrSourcePayloadCorrupt
	}
	complete = err == nil
	return ret, false, err
}

func (s *SourceAttachmentStore) SelectionReview(ctx context.Context, id string) (*models.AttachmentSelectionReview, error) {
	if normalized, err := archiveUUID(id); err != nil || normalized != id {
		return nil, models.ErrAttachmentSelectionReviewInvalid
	}
	return readAttachmentSelectionReview(func(dest any, query string, args ...any) error {
		return dbWrapper.Get(ctx, dest, query, args...)
	}, id)
}

func readAttachmentSelectionReview(get enrichmentGet, id string) (*models.AttachmentSelectionReview, error) {
	var row struct {
		RequestUUID  string    `db:"request_uuid"`
		PostUUID     string    `db:"post_uuid"`
		DecisionUUID string    `db:"decision_uuid"`
		RequestJSON  string    `db:"request_json"`
		Signature    string    `db:"signature"`
		CreatedAt    Timestamp `db:"created_at"`
	}
	if err := get(&row, "SELECT * FROM attachment_selection_reviews WHERE request_uuid=?", id); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var input models.AttachmentSelectionReviewApplyInput
	if err := json.Unmarshal([]byte(row.RequestJSON), &input); err != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	encoded, err := attachmentSelectionReviewRequest(input)
	signature, signatureErr := attachmentSelectionReviewSignature(input, row.DecisionUUID)
	if err != nil || signatureErr != nil || string(encoded) != row.RequestJSON || signature != row.Signature ||
		input.RequestUUID != row.RequestUUID || input.PostUUID != row.PostUUID {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var decision attachmentSelectionRow
	if err := get(&decision, "SELECT * FROM post_attachment_decisions WHERE uuid=?", row.DecisionUUID); err != nil {
		return nil, err
	}
	if decision.PostUUID != input.PostUUID || decision.Mode != input.Mode || decision.Origin != "review" ||
		decision.Reason != input.Reason || decision.Revision-1 != input.PostRevision ||
		decision.CaptureUUID.Valid != (input.Mode != "disabled") || decision.CaptureUUID.String != input.CaptureUUID {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var previous string
	if err := get(&previous, `SELECT coalesce((SELECT uuid FROM post_attachment_decisions
 WHERE post_uuid=? AND revision<? ORDER BY revision DESC LIMIT 1),'')`, row.PostUUID, decision.Revision); err != nil {
		return nil, err
	}
	digest, err := attachmentSelectionReviewDigest(input.AttachmentSelectionReviewInput, previous, decision.Signature)
	if err != nil || digest != input.Digest {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var manifests int
	if err := get(&manifests, "SELECT count(*) FROM post_attachment_decision_manifests WHERE decision_uuid=?", decision.UUID); err != nil {
		return nil, err
	}
	expected := 0
	if input.Mode != "disabled" {
		expected = 1
		var selected bool
		if err := get(&selected, `SELECT EXISTS(SELECT 1 FROM post_attachment_decision_manifests d
 JOIN source_capture_attachment_manifests c ON c.manifest_uuid=d.manifest_uuid
 JOIN source_post_identities owner ON owner.post_uuid=c.post_uuid
 JOIN source_post_identities selected ON selected.post_uuid=d.post_uuid AND selected.canonical_uuid=owner.canonical_uuid
 WHERE d.decision_uuid=? AND c.capture_uuid=?)`, decision.UUID, input.CaptureUUID); err != nil {
			return nil, err
		}
		if !selected {
			return nil, models.ErrSourcePayloadCorrupt
		}
	}
	if manifests != expected || decision.ManifestCount != expected {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.AttachmentSelectionReview{RequestUUID: id, DecisionUUID: row.DecisionUUID, Request: input, CreatedAt: row.CreatedAt.Timestamp}, nil
}
