package sqlite

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

// Only ResolveDiscoveryIdentity calls this after corroborating the exact source
// post against the saved lookup evidence. Shared bytes alone do not identify a
// post: reposts continue sharing library media through the normal first-seen
// metadata and gallery policies.
func (s *EnrichmentJobStore) consolidateDiscoveryDuplicate(ctx context.Context, job, original, existing, basis string, now time.Time) error {
	left, err := currentSourcePost(ctx, original)
	if err != nil {
		return err
	}
	right, err := currentSourcePost(ctx, existing)
	if err != nil {
		return err
	}
	if left == nil || right == nil || left.State != "active" || right.State != "active" {
		return models.ErrEnrichmentIdentityReview
	}
	if left.UUID == right.UUID {
		return nil
	}
	// Keep the first stored post. On equal timestamps retain the existing
	// native identity; source publication dates never replace local metadata.
	if left.CreatedAt.Before(right.CreatedAt) {
		left, right = right, left
	}
	input := models.PostConsolidationReviewInput{SourceUUID: left.UUID, DestinationUUID: right.UUID,
		Media: []models.PostConsolidationMediaChoice{}, Attachments: []models.PostConsolidationAttachmentChoice{},
		Reason: "Keep first stored post; duplicate corroborated by " + basis + " in enrichment job " + job}
	plan, err := preparePostConsolidationReview(ctx, input)
	if err != nil {
		return err
	}
	if !plan.preview.Ready {
		input = firstPostConsolidationChoices(plan.snapshot, input)
		plan, err = preparePostConsolidationReview(ctx, input)
		if err != nil {
			return err
		}
	}
	if !plan.preview.Ready {
		return models.ErrEnrichmentIdentityReview
	}
	_, _, err = (&SourceEvidenceStore{gallery: s.gallery}).ApplyConsolidationReview(ctx,
		models.PostConsolidationReviewApplyInput{PostConsolidationReviewInput: input,
			RequestUUID: uuid.NewSHA1(uuid.MustParse(job), []byte("discovery-duplicate-v1")).String(), Digest: plan.preview.Digest}, now)
	return err
}

// A conflicting later copy does not require the owner to reconstruct source
// slots. Preserve the first post's effective choices, and retain choices that
// exist only on later copies. Compatible automatic lists use the normal union.
func firstPostConsolidationChoices(snapshot *postConsolidationChoiceSnapshot, input models.PostConsolidationReviewInput) models.PostConsolidationReviewInput {
	members := slices.Clone(snapshot.Identity.Members)
	slices.SortFunc(members, func(a, b models.SourcePostIdentity) int {
		if a.UUID == b.UUID {
			return 0
		}
		if order := a.CreatedAt.Compare(b.CreatedAt); order != 0 {
			return order
		}
		if a.UUID == input.DestinationUUID {
			return -1
		}
		if b.UUID == input.DestinationUUID {
			return 1
		}
		return strings.Compare(a.UUID, b.UUID)
	})
	posts := make(map[string]models.SourcePostComparisonState, len(snapshot.Posts))
	for _, post := range snapshot.Posts {
		posts[post.UUID] = post
	}
	media := map[string]bool{}
	attachments := map[models.SourcePostIdentifier]bool{}
	for _, member := range members {
		post := posts[member.UUID]
		if input.Selection == nil && post.Selection != nil {
			input.Selection = &models.PostConsolidationSelectionChoice{Mode: "choose", DecisionUUID: post.Selection.DecisionUUID}
			if post.Selection.Mode == "disabled" {
				input.Selection = &models.PostConsolidationSelectionChoice{Mode: "disabled"}
			}
		}
		if input.Gallery == nil && post.Album != nil {
			input.Gallery = &models.PostConsolidationGalleryChoice{State: post.Album.State}
			if post.Album.Gallery != nil {
				input.Gallery.GalleryUUID = post.Album.Gallery.UUID
			}
		}
		for _, choice := range post.MediaChoices {
			if !media[choice.Media.UUID] {
				input.Media = append(input.Media, models.PostConsolidationMediaChoice{MediaUUID: choice.Media.UUID, State: choice.Decision.State})
				media[choice.Media.UUID] = true
			}
		}
		for _, attachment := range post.Attachments {
			ref := models.SourcePostIdentifier{Namespace: attachment.Namespace, Value: attachment.Value}
			if attachments[ref] || attachment.Choice == nil {
				continue
			}
			choice := models.PostConsolidationAttachmentChoice{Namespace: ref.Namespace, Value: ref.Value, State: attachment.Choice.State}
			if attachment.Choice.MediaUUID != nil {
				choice.MediaUUID = *attachment.Choice.MediaUUID
			}
			input.Attachments = append(input.Attachments, choice)
			attachments[ref] = true
		}
	}
	return input
}
