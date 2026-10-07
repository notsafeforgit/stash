package sqlite

import (
	"context"
	"encoding/json"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

func attachmentMediaDecisionView(value *models.AttachmentMediaDecision) *models.AttachmentMediaReviewDecision {
	if value == nil {
		return nil
	}
	return &models.AttachmentMediaReviewDecision{UUID: value.UUID, AttachmentUUID: value.AttachmentUUID, Revision: value.Revision,
		State: value.State, MediaUUID: value.MediaUUID, Origin: value.Origin, Reason: value.Reason, CreatedAt: value.CreatedAt}
}

func (s *SourceAttachmentStore) MediaReviewHistory(ctx context.Context, id string, after, limit int) ([]models.AttachmentMediaReviewDecision, error) {
	values, err := s.MediaDecisionHistory(ctx, id, after, limit)
	if err != nil {
		return nil, err
	}
	ret := make([]models.AttachmentMediaReviewDecision, 0, len(values))
	for _, value := range values {
		ret = append(ret, *attachmentMediaDecisionView(&value))
	}
	return ret, nil
}

const currentAttachmentMediaKindsQuery = `SELECT DISTINCT e.media_kind FROM source_attachments requested
 JOIN source_post_identities root ON root.post_uuid=requested.post_uuid
 JOIN post_attachment_selections h ON h.post_uuid=root.canonical_uuid
 JOIN post_attachment_decisions d ON d.uuid=h.decision_uuid AND d.mode!='disabled'
 JOIN post_attachment_decision_manifests m ON m.decision_uuid=d.uuid
 JOIN source_attachment_entries e ON e.manifest_uuid=m.manifest_uuid
 JOIN source_attachments a ON a.uuid=e.attachment_uuid
 WHERE requested.uuid=? AND a.namespace=requested.namespace AND a.value=requested.value
 AND e.media_kind!='unknown' ORDER BY e.media_kind LIMIT 2`

func (s *SourceAttachmentStore) MediaReviewContext(ctx context.Context, id string) (*models.AttachmentMediaReviewContext, error) {
	if !validAssociationReviewUUID(id) {
		return nil, models.ErrSourceAssociationReviewInvalid
	}
	attachment, err := s.Find(ctx, id)
	if err != nil || attachment == nil {
		return nil, err
	}
	decision, err := s.MediaDecision(ctx, id)
	if err != nil {
		return nil, err
	}
	if decision != nil && decision.AttachmentUUID != attachment.UUID {
		attachment, err = s.Find(ctx, decision.AttachmentUUID)
		if err != nil {
			return nil, err
		}
		if attachment == nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
	}
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, attachment.PostUUID)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	ret := &models.AttachmentMediaReviewContext{RequestedAttachmentUUID: id, PostUUID: post.UUID, PostRevision: post.Revision, PostState: post.State,
		Attachment: models.SourceAlbumAttachment{UUID: attachment.UUID, Revision: attachment.Revision, Reference: models.SourcePostIdentifierSummary{
			Namespace: attachment.Reference.Namespace, Value: attachment.Reference.Value}}, Current: attachmentMediaDecisionView(decision), SourceMediaKinds: []string{}}
	// These are hints from the currently selected source order, not restrictions
	// on the chosen library type: postprocessing can convert an image to video.
	if err := dbWrapper.Select(ctx, &ret.SourceMediaKinds, currentAttachmentMediaKindsQuery, id); err != nil {
		return nil, err
	}
	if decision != nil && decision.MediaUUID != nil {
		media, err := (&ArchiveEntityStore{}).Resolve(ctx, *decision.MediaUUID)
		if err != nil {
			return nil, err
		}
		if !archiveMedia(media) {
			return nil, models.ErrSourcePayloadCorrupt
		}
		ret.Media, err = sourcePostLibraryItem(ctx, media)
		if err != nil {
			return nil, err
		}
		association, err := (&SourcePostMediaStore{}).Association(ctx, post.UUID, media.UUID)
		if err != nil {
			return nil, err
		}
		ret.PostLinkState = association.State
	}
	return ret, nil
}

func validateAttachmentMediaReview(input models.AttachmentMediaReviewInput) error {
	if !validAssociationReviewUUID(input.PostUUID) || !validAssociationReviewUUID(input.AttachmentUUID) ||
		input.PostRevision < 1 || input.AttachmentRevision < 1 || !validAccountText(input.Reason, 4096, true) {
		return models.ErrSourceAssociationReviewInvalid
	}
	switch input.State {
	case "linked":
		if !validAssociationReviewUUID(input.MediaUUID) || input.MediaRevision < 1 {
			return models.ErrSourceAssociationReviewInvalid
		}
	case "unlinked", "undecided":
		if input.MediaUUID != "" || input.MediaRevision != 0 {
			return models.ErrSourceAssociationReviewInvalid
		}
	default:
		return models.ErrSourceAssociationReviewInvalid
	}
	return nil
}

func attachmentMediaReviewChoice(input models.AttachmentMediaReviewInput) models.AttachmentMediaDecisionInput {
	return models.AttachmentMediaDecisionInput{AttachmentUUID: input.AttachmentUUID, ExpectedAttachmentRevision: input.AttachmentRevision,
		State: input.State, MediaUUID: input.MediaUUID, ExpectedMediaRevision: input.MediaRevision, Origin: "review", Reason: input.Reason}
}

func attachmentMediaReviewDigest(input models.AttachmentMediaReviewInput, previous string) (string, error) {
	return sourceSignature("stash-attachment-media-review-v1", struct {
		Input            models.AttachmentMediaReviewInput `json:"input"`
		PreviousDecision string                            `json:"previous_decision"`
	}{input, previous})
}

func (s *SourceAttachmentStore) PreviewMediaReview(ctx context.Context, input models.AttachmentMediaReviewInput) (*models.AttachmentMediaReviewPreview, error) {
	if err := validateAttachmentMediaReview(input); err != nil {
		return nil, err
	}
	current, err := s.MediaReviewContext(ctx, input.AttachmentUUID)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Attachment.UUID != input.AttachmentUUID || current.PostUUID != input.PostUUID || current.PostRevision != input.PostRevision || current.Attachment.Revision != input.AttachmentRevision {
		return nil, models.ErrSourceAssociationReviewConflict
	}
	_, target, err := s.prepareMediaChoice(ctx, attachmentMediaReviewChoice(input))
	if err != nil {
		return nil, err
	}
	ret := &models.AttachmentMediaReviewPreview{Input: input, Current: *current, Changed: true}
	if target != nil {
		ret.Proposed, err = sourcePostLibraryItem(ctx, target)
		if err != nil {
			return nil, err
		}
	}
	previous := ""
	if current.Current != nil {
		d := current.Current
		previous = d.UUID
		selected := ""
		if d.MediaUUID != nil {
			selected = *d.MediaUUID
		}
		ret.Changed = d.State != input.State || selected != input.MediaUUID || d.Origin != "review" || d.Reason != input.Reason
	}
	ret.Digest, err = attachmentMediaReviewDigest(input, previous)
	return ret, err
}

func attachmentMediaReviewRequest(input models.AttachmentMediaReviewApplyInput) ([]byte, error) {
	if err := validateAttachmentMediaReview(input.AttachmentMediaReviewInput); err != nil {
		return nil, err
	}
	if !validAssociationReviewUUID(input.RequestUUID) || !archive.ValidSHA256(input.Digest) {
		return nil, models.ErrSourceAssociationReviewInvalid
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > 16384 {
		return nil, models.ErrSourceAssociationReviewInvalid
	}
	return encoded, nil
}

func (s *SourceAttachmentStore) ApplyMediaReview(ctx context.Context, input models.AttachmentMediaReviewApplyInput) (*models.AttachmentMediaReview, bool, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, false, err
	}
	encoded, err := attachmentMediaReviewRequest(input)
	if err != nil {
		return nil, false, err
	}
	prior, err := s.MediaReview(ctx, input.RequestUUID)
	if err != nil {
		return nil, false, err
	}
	if prior != nil {
		previous, err := attachmentMediaReviewRequest(prior.Request)
		if err != nil {
			return nil, false, err
		}
		if string(previous) != string(encoded) {
			return nil, false, models.ErrSourceAssociationReviewReplay
		}
		return prior, true, nil
	}
	preview, err := s.PreviewMediaReview(ctx, input.AttachmentMediaReviewInput)
	if err != nil {
		return nil, false, err
	}
	if preview.Digest != input.Digest {
		return nil, false, models.ErrSourceAssociationReviewConflict
	}
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrSourceAssociationReviewInvalid
		}
		return nil
	})
	decision, err := s.DecideMedia(ctx, attachmentMediaReviewChoice(input.AttachmentMediaReviewInput))
	if err != nil {
		return nil, false, err
	}
	signature, err := sourceAssociationReceiptSignature("attachment-media", input, decision.UUID)
	if err != nil {
		return nil, false, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO attachment_media_reviews(request_uuid,post_uuid,attachment_uuid,decision_uuid,request_json,signature)
 VALUES(?,?,?,?,?,?)`, input.RequestUUID, input.PostUUID, input.AttachmentUUID, decision.UUID, string(encoded), signature); err != nil {
		return nil, false, err
	}
	result, err := s.MediaReview(ctx, input.RequestUUID)
	if err == nil && result == nil {
		err = models.ErrSourcePayloadCorrupt
	}
	complete = err == nil
	return result, false, err
}

func (s *SourceAttachmentStore) MediaReview(ctx context.Context, id string) (*models.AttachmentMediaReview, error) {
	return readAttachmentMediaReview(func(dest any, query string, args ...any) error { return dbWrapper.Get(ctx, dest, query, args...) }, id)
}

func readAttachmentMediaReview(get enrichmentGet, id string) (*models.AttachmentMediaReview, error) {
	row, err := readSourceAssociationReviewRow(get, "attachment_media_reviews", id)
	if err != nil || row == nil {
		return nil, err
	}
	var input models.AttachmentMediaReviewApplyInput
	if err := json.Unmarshal([]byte(row.RequestJSON), &input); err != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	encoded, err := attachmentMediaReviewRequest(input)
	signature, signatureErr := sourceAssociationReceiptSignature("attachment-media", input, row.DecisionUUID)
	if err != nil || signatureErr != nil || string(encoded) != row.RequestJSON || signature != row.Signature || input.RequestUUID != row.RequestUUID ||
		input.PostUUID != row.PostUUID || input.AttachmentUUID != row.AttachmentUUID {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var decision attachmentMediaDecisionRow
	if err := get(&decision, "SELECT * FROM attachment_media_decisions WHERE uuid=?", row.DecisionUUID); err != nil {
		return nil, err
	}
	if decision.AttachmentUUID != input.AttachmentUUID || decision.Revision-1 != input.AttachmentRevision || decision.State != input.State ||
		decision.Origin != "review" || decision.Reason != input.Reason || decision.MediaUUID.Valid != (input.State == "linked") {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var post string
	if err := get(&post, "SELECT post_uuid FROM source_attachments WHERE uuid=?", input.AttachmentUUID); err != nil {
		return nil, err
	}
	if post != input.PostUUID {
		return nil, models.ErrSourcePayloadCorrupt
	}
	if input.State == "linked" {
		same, err := sameReviewArchiveIdentity(get, input.MediaUUID, decision.MediaUUID.String)
		if err != nil {
			return nil, err
		}
		if !same {
			return nil, models.ErrSourcePayloadCorrupt
		}
	}
	var previous string
	if err := get(&previous, `SELECT coalesce((SELECT uuid FROM attachment_media_decisions
 WHERE attachment_uuid=? AND revision<? ORDER BY revision DESC LIMIT 1),'')`, row.AttachmentUUID, decision.Revision); err != nil {
		return nil, err
	}
	digest, err := attachmentMediaReviewDigest(input.AttachmentMediaReviewInput, previous)
	if err != nil || digest != input.Digest {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.AttachmentMediaReview{RequestUUID: id, DecisionUUID: row.DecisionUUID, Request: input, CreatedAt: row.CreatedAt.Timestamp}, nil
}
