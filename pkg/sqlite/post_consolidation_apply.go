package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func postConsolidationReviewRequest(input models.PostConsolidationReviewApplyInput) ([]byte, error) {
	if err := validatePostConsolidationReview(input.PostConsolidationReviewInput); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(input.RequestUUID) || !archive.ValidSHA256(input.Digest) {
		return nil, models.ErrPostConsolidationReviewInvalid
	}
	body, err := json.Marshal(input)
	if err != nil || len(body) > models.MaxPostConsolidationReviewBytes {
		return nil, models.ErrPostConsolidationReviewInvalid
	}
	return body, nil
}

func (s *SourceEvidenceStore) PreviewConsolidationReview(ctx context.Context, input models.PostConsolidationReviewInput) (*models.PostConsolidationReviewPreview, error) {
	plan, err := preparePostConsolidationReview(ctx, input)
	if err != nil {
		return nil, err
	}
	return plan.preview, nil
}

func (s *SourceEvidenceStore) ApplyConsolidationReview(ctx context.Context, input models.PostConsolidationReviewApplyInput, now time.Time) (*models.PostConsolidationReview, bool, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, false, err
	}
	encoded, err := postConsolidationReviewRequest(input)
	if err != nil {
		return nil, false, err
	}
	prior, err := s.CheckConsolidationReview(ctx, input)
	if err != nil {
		return nil, false, err
	}
	if prior != nil {
		return prior, true, nil
	}
	if !validJobTime(now) {
		return nil, false, models.ErrPostConsolidationReviewInvalid
	}
	plan, err := preparePostConsolidationReview(ctx, input.PostConsolidationReviewInput)
	if err != nil {
		return nil, false, err
	}
	if !plan.preview.Ready || input.Digest != plan.preview.Digest {
		return nil, false, models.ErrPostConsolidationReviewConflict
	}
	finish := postConsolidationCommitGuard(ctx, models.ErrPostConsolidationReviewConflict)
	merged, err := publishPostConsolidationIdentity(ctx, postIdentityConsolidationInput{UUID: input.RequestUUID,
		SourceUUID: input.SourceUUID, DestinationUUID: input.DestinationUUID, IdentitySignature: plan.snapshot.Identity.Signature,
		ReviewSignature: input.Digest, Origin: "review", Reason: input.Reason})
	if err != nil {
		return nil, false, err
	}
	result := models.PostConsolidationReviewResult{Consolidation: *merged, Members: []models.PostConsolidationReviewMember{},
		MediaDecisionUUIDs: []string{}, AttachmentDecisionUUIDs: []string{}}
	for _, member := range plan.snapshot.Identity.Members {
		result.Members = append(result.Members, models.PostConsolidationReviewMember{PostUUID: member.UUID,
			PreviousCanonicalUUID: member.CanonicalUUID, PreviousRevision: member.Revision})
	}
	slices.SortFunc(result.Members, func(a, b models.PostConsolidationReviewMember) int { return strings.Compare(a.PostUUID, b.PostUUID) })
	if err := plan.publishSelection(ctx, input, &result); err != nil {
		return nil, false, err
	}
	if err := plan.publishMediaChoices(ctx, input, &result); err != nil {
		return nil, false, err
	}
	if err := plan.publishAttachmentChoices(ctx, input, &result); err != nil {
		return nil, false, err
	}
	if err := plan.publishGalleryChoice(ctx, input); err != nil {
		return nil, false, err
	}
	if err := s.publishConsolidatedAlbum(ctx, input, plan, &result); err != nil {
		return nil, false, fmt.Errorf("publish consolidated album: %w", err)
	}
	if result.Gallery.Changed() {
		job, err := submitPostMergeNotification(ctx, input.RequestUUID, result.Gallery.GalleryUUID,
			uuid.NewSHA1(uuid.MustParse(input.RequestUUID), []byte("post-merge-notification")).String(), now)
		if err != nil {
			return nil, false, err
		}
		result.NotificationJobUUID = job.UUID
	}
	review := &models.PostConsolidationReview{Request: input, Result: result}
	if err := storePostConsolidationReview(ctx, review, encoded); err != nil {
		return nil, false, fmt.Errorf("save post merge review: %w", err)
	}
	ret, err := s.ConsolidationReview(ctx, input.RequestUUID)
	if err != nil {
		return nil, false, fmt.Errorf("verify saved post merge review: %w", err)
	}
	if ret == nil || !reflect.DeepEqual(ret, review) {
		return nil, false, fmt.Errorf("saved post merge result differs: %w", models.ErrSourcePayloadCorrupt)
	}
	finish()
	return ret, false, nil
}

func (p *postConsolidationPlan) publishSelection(ctx context.Context, input models.PostConsolidationReviewApplyInput, result *models.PostConsolidationReviewResult) error {
	if p.selection == nil {
		return nil
	}
	post, err := currentSourcePost(ctx, input.DestinationUUID)
	if err != nil {
		return err
	}
	choice := models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: post.Revision,
		Mode: p.selection.Decision.Mode, Origin: "review", Reason: input.Reason}
	if p.selection.Decision.CaptureUUID != nil {
		choice.CaptureUUID = *p.selection.Decision.CaptureUUID
	}
	selected, err := publishConsolidatedPostSelection(ctx, choice, input.RequestUUID, sortedPostConsolidationKeys(p.snapshot.selections), p.selection.Decision.ManifestUUIDs)
	if err == nil {
		result.SelectionUUID = selected.Decision.UUID
	}
	return err
}

func (p *postConsolidationPlan) publishMediaChoices(ctx context.Context, input models.PostConsolidationReviewApplyInput, result *models.PostConsolidationReviewResult) error {
	for _, chosen := range p.preview.Media {
		current, err := (&SourcePostMediaStore{}).Association(ctx, input.DestinationUUID, chosen.MediaUUID)
		if err != nil {
			return err
		}
		choice := models.SourcePostMediaInput{UUID: uuid.NewSHA1(uuid.MustParse(input.RequestUUID), []byte("post-media\x00"+chosen.MediaUUID)).String(),
			PostUUID: input.DestinationUUID, MediaUUID: chosen.MediaUUID, ExpectedPostRevision: current.PostRevision, ExpectedMediaRevision: current.MediaRevision,
			ExpectedDecisions: []string{}, State: chosen.State, Origin: "review", Reason: input.Reason}
		for _, decision := range current.Decisions {
			choice.ExpectedDecisions = append(choice.ExpectedDecisions, decision.UUID)
		}
		decision, err := publishConsolidatedPostMedia(ctx, choice, input.RequestUUID)
		if err != nil {
			return err
		}
		result.MediaDecisionUUIDs = append(result.MediaDecisionUUIDs, decision.UUID)
	}
	return nil
}

func (p *postConsolidationPlan) publishAttachmentChoices(ctx context.Context, input models.PostConsolidationReviewApplyInput, result *models.PostConsolidationReviewResult) error {
	for _, chosen := range p.preview.Attachments {
		current, err := inspectPostAttachmentChoices(ctx, input.DestinationUUID, models.SourcePostIdentifier{Namespace: chosen.Namespace, Value: chosen.Value})
		if err != nil {
			return err
		}
		owner := current.Members[0].Attachment
		choice := models.AttachmentMediaDecisionInput{AttachmentUUID: owner.UUID, ExpectedAttachmentRevision: owner.Revision,
			State: chosen.State, MediaUUID: chosen.MediaUUID, Origin: "review", Reason: input.Reason}
		if chosen.State == "linked" {
			entity, err := (&ArchiveEntityStore{}).Find(ctx, chosen.MediaUUID)
			if err != nil {
				return err
			}
			if entity == nil {
				return models.ErrSourceAttachmentConflict
			}
			choice.ExpectedMediaRevision = entity.Revision
		}
		decision, err := publishConsolidatedAttachmentMedia(ctx, input.DestinationUUID, input.RequestUUID, current.Signature, choice)
		if err != nil {
			return err
		}
		result.AttachmentDecisionUUIDs = append(result.AttachmentDecisionUUIDs, decision.UUID)
	}
	return nil
}

func (p *postConsolidationPlan) publishGalleryChoice(ctx context.Context, input models.PostConsolidationReviewApplyInput) error {
	if p.preview.Gallery == nil {
		return nil
	}
	post, err := currentSourcePost(ctx, input.DestinationUUID)
	if err != nil {
		return err
	}
	choice := models.SourceGalleryChoiceInput{PostUUID: post.UUID, ExpectedPostRevision: post.Revision,
		State: p.preview.Gallery.State, GalleryUUID: p.preview.Gallery.GalleryUUID, Origin: "review", Reason: input.Reason}
	if choice.State == "linked" {
		entity, err := (&ArchiveEntityStore{}).Find(ctx, choice.GalleryUUID)
		if err != nil {
			return err
		}
		if entity == nil {
			return models.ErrSourceGalleryConflict
		}
		choice.ExpectedGalleryRevision = entity.Revision
	}
	expected := []string{}
	for _, post := range p.snapshot.Posts {
		if post.Album != nil {
			expected = append(expected, post.Album.DecisionUUID)
		}
	}
	_, err = publishConsolidatedPostGallery(ctx, choice, input.RequestUUID, expected)
	return err
}

func (s *SourceEvidenceStore) publishConsolidatedAlbum(ctx context.Context, input models.PostConsolidationReviewApplyInput, planned *postConsolidationPlan, result *models.PostConsolidationReviewResult) error {
	gallery := &SourceGalleryStore{gallery: s.gallery}
	preview, err := gallery.Preview(ctx, input.DestinationUUID)
	if err != nil {
		return err
	}
	selection, err := (&SourceAttachmentStore{}).Selection(ctx, input.DestinationUUID)
	if err != nil {
		return err
	}
	actual := &postConsolidationPlan{preview: &models.PostConsolidationReviewPreview{}, selection: selection}
	if err := actual.setAlbumPlan(preview); err != nil {
		return err
	}
	if !reflect.DeepEqual(actual.preview.Album, planned.preview.Album) {
		return models.ErrPostConsolidationReviewConflict
	}
	published, err := gallery.Sync(ctx, input.DestinationUUID, preview.Signature)
	if err != nil {
		return err
	}
	result.Gallery = models.PostConsolidationGalleryResult{GalleryUUID: published.GalleryUUID, Action: published.Action,
		Created: published.Created, Added: published.Added, Removed: published.Removed}
	association, err := gallery.Association(ctx, input.DestinationUUID)
	if err == nil && association != nil {
		result.GalleryDecisionUUID = association.UUID
	}
	return err
}
