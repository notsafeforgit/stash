package sqlite

import (
	"context"
	"encoding/json"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

func validateGalleryAssociationReview(input models.GalleryAssociationReviewInput) error {
	if !validAssociationReviewUUID(input.PostUUID) || input.PostRevision < 1 || !validAccountText(input.Reason, 4096, true) {
		return models.ErrSourceAssociationReviewInvalid
	}
	switch input.State {
	case "linked":
		if !validAssociationReviewUUID(input.GalleryUUID) || input.GalleryRevision < 1 {
			return models.ErrSourceAssociationReviewInvalid
		}
	case "disabled":
		if input.GalleryUUID != "" || input.GalleryRevision != 0 {
			return models.ErrSourceAssociationReviewInvalid
		}
	default:
		return models.ErrSourceAssociationReviewInvalid
	}
	return nil
}

func galleryAssociationReviewChoice(input models.GalleryAssociationReviewInput) models.SourceGalleryChoiceInput {
	return models.SourceGalleryChoiceInput{PostUUID: input.PostUUID, ExpectedPostRevision: input.PostRevision, State: input.State,
		GalleryUUID: input.GalleryUUID, ExpectedGalleryRevision: input.GalleryRevision, Origin: "review", Reason: input.Reason}
}

func galleryAssociationReviewDigest(input models.GalleryAssociationReviewInput, previous string) (string, error) {
	return sourceSignature("stash-gallery-association-review-v1", struct {
		Input            models.GalleryAssociationReviewInput `json:"input"`
		PreviousDecision string                               `json:"previous_decision"`
	}{input, previous})
}

func (s *SourceGalleryStore) PreviewAssociationReview(ctx context.Context, input models.GalleryAssociationReviewInput) (*models.GalleryAssociationReviewPreview, error) {
	if err := validateGalleryAssociationReview(input); err != nil {
		return nil, err
	}
	_, target, err := s.prepareAssociationChoice(ctx, galleryAssociationReviewChoice(input))
	if err != nil {
		return nil, err
	}
	current, err := s.AssociationView(ctx, input.PostUUID)
	if err != nil {
		return nil, err
	}
	ret := &models.GalleryAssociationReviewPreview{Input: input, Current: current, Changed: true}
	if target != nil {
		ret.Proposed, err = sourcePostLibraryItem(ctx, target)
		if err != nil {
			return nil, err
		}
	}
	previous := ""
	if current != nil {
		previous = current.DecisionUUID
		selected := ""
		if current.GalleryUUID != nil {
			selected = *current.GalleryUUID
		}
		ret.Changed = current.State != input.State || selected != input.GalleryUUID || current.Origin != "review" || current.Reason != input.Reason
	}
	ret.Digest, err = galleryAssociationReviewDigest(input, previous)
	return ret, err
}

func galleryAssociationReviewRequest(input models.GalleryAssociationReviewApplyInput) ([]byte, error) {
	if err := validateGalleryAssociationReview(input.GalleryAssociationReviewInput); err != nil {
		return nil, err
	}
	if !validAssociationReviewUUID(input.RequestUUID) || !archive.ValidSHA256(input.Digest) {
		return nil, models.ErrSourceAssociationReviewInvalid
	}
	data, err := json.Marshal(input)
	if err != nil || len(data) > 16384 {
		return nil, models.ErrSourceAssociationReviewInvalid
	}
	return data, nil
}

func (s *SourceGalleryStore) ApplyAssociationReview(ctx context.Context, input models.GalleryAssociationReviewApplyInput) (*models.GalleryAssociationReview, bool, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, false, err
	}
	encoded, err := galleryAssociationReviewRequest(input)
	if err != nil {
		return nil, false, err
	}
	prior, err := s.AssociationReview(ctx, input.RequestUUID)
	if err != nil {
		return nil, false, err
	}
	if prior != nil {
		previous, err := galleryAssociationReviewRequest(prior.Request)
		if err != nil {
			return nil, false, err
		}
		if string(previous) != string(encoded) {
			return nil, false, models.ErrSourceAssociationReviewReplay
		}
		return prior, true, nil
	}
	preview, err := s.PreviewAssociationReview(ctx, input.GalleryAssociationReviewInput)
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
	decision, err := s.DecideAssociation(ctx, galleryAssociationReviewChoice(input.GalleryAssociationReviewInput))
	if err != nil {
		return nil, false, err
	}
	signature, err := sourceAssociationReceiptSignature("gallery-association", input, decision.UUID)
	if err != nil {
		return nil, false, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO gallery_association_reviews(request_uuid,post_uuid,decision_uuid,request_json,signature)
 VALUES(?,?,?,?,?)`, input.RequestUUID, input.PostUUID, decision.UUID, string(encoded), signature); err != nil {
		return nil, false, err
	}
	result, err := s.AssociationReview(ctx, input.RequestUUID)
	if err == nil && result == nil {
		err = models.ErrSourcePayloadCorrupt
	}
	complete = err == nil
	return result, false, err
}

func (s *SourceGalleryStore) AssociationReview(ctx context.Context, id string) (*models.GalleryAssociationReview, error) {
	return readGalleryAssociationReview(func(dest any, query string, args ...any) error { return dbWrapper.Get(ctx, dest, query, args...) }, id)
}

func readGalleryAssociationReview(get enrichmentGet, id string) (*models.GalleryAssociationReview, error) {
	row, err := readSourceAssociationReviewRow(get, "gallery_association_reviews", id)
	if err != nil || row == nil {
		return nil, err
	}
	var input models.GalleryAssociationReviewApplyInput
	if err := json.Unmarshal([]byte(row.RequestJSON), &input); err != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	encoded, err := galleryAssociationReviewRequest(input)
	signature, signatureErr := sourceAssociationReceiptSignature("gallery-association", input, row.DecisionUUID)
	if err != nil || signatureErr != nil || string(encoded) != row.RequestJSON || signature != row.Signature ||
		input.RequestUUID != row.RequestUUID || input.PostUUID != row.PostUUID {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var decision sourceGalleryDecisionRow
	if err := get(&decision, "SELECT * FROM post_gallery_decisions WHERE uuid=?", row.DecisionUUID); err != nil {
		return nil, err
	}
	if decision.PostUUID != input.PostUUID || decision.Revision-1 != input.PostRevision || decision.State != input.State ||
		decision.Origin != "review" || decision.Reason != input.Reason || decision.SelectionUUID.Valid ||
		decision.GalleryUUID.Valid != (input.State == "linked") {
		return nil, models.ErrSourcePayloadCorrupt
	}
	if input.State == "linked" {
		same, err := sameReviewArchiveIdentity(get, input.GalleryUUID, decision.GalleryUUID.String)
		if err != nil {
			return nil, err
		}
		if !same {
			return nil, models.ErrSourcePayloadCorrupt
		}
	}
	var previous string
	if err := get(&previous, `SELECT coalesce((SELECT uuid FROM post_gallery_decisions
 WHERE post_uuid=? AND revision<? ORDER BY revision DESC LIMIT 1),'')`, row.PostUUID, decision.Revision); err != nil {
		return nil, err
	}
	digest, err := galleryAssociationReviewDigest(input.GalleryAssociationReviewInput, previous)
	if err != nil || digest != input.Digest {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.GalleryAssociationReview{RequestUUID: id, DecisionUUID: row.DecisionUUID, Request: input, CreatedAt: row.CreatedAt.Timestamp}, nil
}
