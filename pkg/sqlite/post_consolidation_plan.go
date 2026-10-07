package sqlite

import (
	"context"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

type postConsolidationPlan struct {
	snapshot  *postConsolidationChoiceSnapshot
	preview   *models.PostConsolidationReviewPreview
	selection *models.AttachmentSelection
	gallery   *models.SourceGalleryPreview
}

func validPostMergeChoice(state string) bool {
	return state == "linked" || state == "unlinked" || state == "undecided"
}

func validatePostConsolidationReview(input models.PostConsolidationReviewInput) error {
	invalid := models.ErrPostConsolidationReviewInvalid
	if !validSourceRunUUID(input.SourceUUID) || !validSourceRunUUID(input.DestinationUUID) ||
		input.SourceUUID == input.DestinationUUID || !validAccountText(input.Reason, 4096, true) ||
		len(input.Media) > maxPostComparisonChoices || len(input.Attachments) > maxPostComparisonChoices {
		return invalid
	}
	if choice := input.Selection; choice != nil {
		switch choice.Mode {
		case "choose", "combine":
			if !validSourceRunUUID(choice.DecisionUUID) {
				return invalid
			}
		case "disabled":
			if choice.DecisionUUID != "" {
				return invalid
			}
		default:
			return invalid
		}
	}
	if choice := input.Gallery; choice != nil {
		if (choice.State != "linked" && choice.State != "disabled") ||
			(choice.State == "linked" && !validSourceRunUUID(choice.GalleryUUID)) ||
			(choice.State == "disabled" && choice.GalleryUUID != "") {
			return invalid
		}
	}
	media := map[string]bool{}
	for _, choice := range input.Media {
		if !validSourceRunUUID(choice.MediaUUID) || !validPostMergeChoice(choice.State) || media[choice.MediaUUID] {
			return invalid
		}
		media[choice.MediaUUID] = true
	}
	attachments := map[models.SourcePostIdentifier]bool{}
	for _, choice := range input.Attachments {
		ref := models.SourcePostIdentifier{Namespace: choice.Namespace, Value: choice.Value}
		if validatePostIdentifier(ref) != nil || attachments[ref] || !validPostMergeChoice(choice.State) ||
			(choice.State == "linked" && !validSourceRunUUID(choice.MediaUUID)) ||
			(choice.State != "linked" && choice.MediaUUID != "") {
			return invalid
		}
		attachments[ref] = true
	}
	return nil
}

func preparePostConsolidationReview(ctx context.Context, input models.PostConsolidationReviewInput) (*postConsolidationPlan, error) {
	if err := validatePostConsolidationReview(input); err != nil {
		return nil, err
	}
	snapshot, err := inspectPostConsolidationChoices(ctx, input.SourceUUID, input.DestinationUUID)
	if err != nil {
		return nil, err
	}
	view := &models.PostConsolidationReviewPreview{Source: snapshot.Identity.Source, Destination: snapshot.Identity.Destination,
		Posts: snapshot.Posts, Media: []models.PostConsolidationMediaChoice{}, Attachments: []models.PostConsolidationAttachmentChoice{},
		Blockers: []models.PostConsolidationReviewBlocker{}}
	plan := &postConsolidationPlan{snapshot: snapshot, preview: view}
	if err := plan.prepareSelection(ctx, input.Selection); err != nil {
		return nil, err
	}
	if err := plan.prepareGallery(input.Gallery); err != nil {
		return nil, err
	}
	if err := plan.prepareMedia(input.Media); err != nil {
		return nil, err
	}
	if err := plan.prepareAttachments(input.Attachments); err != nil {
		return nil, err
	}
	if err := plan.prepareAlbum(ctx); err != nil {
		return nil, err
	}
	view.Ready = len(view.Blockers) == 0
	view.Digest, err = sourceSignature("stash-post-consolidation-review-v1", []any{snapshot.Signature, input, view})
	return plan, err
}

func (p *postConsolidationPlan) block(kind, namespace, value, media string) {
	p.preview.Blockers = append(p.preview.Blockers, models.PostConsolidationReviewBlocker{Kind: kind, Namespace: namespace, Value: value, MediaUUID: media})
}

func (p *postConsolidationPlan) prepareGallery(chosen *models.PostConsolidationGalleryChoice) error {
	choices := map[string]models.PostConsolidationGalleryChoice{}
	galleries := map[string]*models.SourcePostLibraryItem{}
	for _, post := range p.snapshot.Posts {
		if post.Album == nil {
			continue
		}
		choice := models.PostConsolidationGalleryChoice{State: post.Album.State}
		if post.Album.Gallery != nil {
			choice.GalleryUUID = post.Album.Gallery.UUID
			galleries[choice.GalleryUUID] = post.Album.Gallery
		}
		choices[choice.State+":"+choice.GalleryUUID] = choice
	}
	if chosen == nil {
		if len(choices) > 1 {
			p.block("gallery_choice", "", "", "")
			return nil
		}
		for _, choice := range choices {
			selected := choice
			chosen = &selected
		}
	}
	if chosen == nil {
		return nil
	}
	if chosen.State == "linked" {
		gallery := galleries[chosen.GalleryUUID]
		if gallery == nil {
			return models.ErrSourceGalleryConflict
		}
		if gallery.State != models.ArchiveEntityActive {
			p.block("gallery_unavailable", "", "", gallery.UUID)
		}
	}
	selected := *chosen
	p.preview.Gallery = &selected
	return nil
}

func (p *postConsolidationPlan) prepareMedia(overrides []models.PostConsolidationMediaChoice) error {
	groups := map[string][]models.SourcePostComparisonMedia{}
	for _, post := range p.snapshot.Posts {
		for _, choice := range post.MediaChoices {
			groups[choice.Media.UUID] = append(groups[choice.Media.UUID], choice)
		}
	}
	explicit := map[string]string{}
	for _, choice := range overrides {
		if groups[choice.MediaUUID] == nil {
			return models.ErrSourcePostMediaConflict
		}
		explicit[choice.MediaUUID] = choice.State
	}
	for _, id := range sortedPostConsolidationKeys(groups) {
		heads := groups[id]
		states := map[string]bool{}
		for _, head := range heads {
			states[head.Decision.State] = true
		}
		state := explicit[id]
		if state == "" {
			if len(states) != 1 {
				p.block("media_choice", "", "", id)
				continue
			}
			for current := range states {
				state = current
			}
		}
		if state == "linked" && heads[0].Media.State != models.ArchiveEntityActive && !states["linked"] {
			p.block("media_unavailable", "", "", id)
		}
		p.preview.Media = append(p.preview.Media, models.PostConsolidationMediaChoice{MediaUUID: id, State: state})
	}
	return nil
}

func (p *postConsolidationPlan) prepareAttachments(overrides []models.PostConsolidationAttachmentChoice) error {
	groups := map[models.SourcePostIdentifier][]models.SourcePostComparisonAttachment{}
	for _, post := range p.snapshot.Posts {
		for _, attachment := range post.Attachments {
			ref := models.SourcePostIdentifier{Namespace: attachment.Namespace, Value: attachment.Value}
			groups[ref] = append(groups[ref], attachment)
		}
	}
	explicit := map[models.SourcePostIdentifier]models.PostConsolidationAttachmentChoice{}
	for _, choice := range overrides {
		ref := models.SourcePostIdentifier{Namespace: choice.Namespace, Value: choice.Value}
		if groups[ref] == nil {
			return models.ErrSourceAttachmentConflict
		}
		explicit[ref] = choice
	}
	keys := make([]models.SourcePostIdentifier, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b models.SourcePostIdentifier) int {
		if order := strings.Compare(a.Namespace, b.Namespace); order != 0 {
			return order
		}
		return strings.Compare(a.Value, b.Value)
	})
	postStates := map[string]string{}
	for _, choice := range p.preview.Media {
		postStates[choice.MediaUUID] = choice.State
	}
	for _, ref := range keys {
		choices := map[string]models.PostConsolidationAttachmentChoice{}
		candidates := map[string]bool{}
		for _, member := range groups[ref] {
			if member.Choice == nil {
				continue
			}
			choice := models.PostConsolidationAttachmentChoice{Namespace: ref.Namespace, Value: ref.Value, State: member.Choice.State}
			if member.Media != nil {
				choice.MediaUUID = member.Media.UUID
				candidates[choice.MediaUUID] = true
			}
			choices[choice.State+":"+choice.MediaUUID] = choice
		}
		chosen, hasOverride := explicit[ref]
		if !hasOverride {
			if len(choices) == 0 {
				continue
			}
			if len(choices) != 1 {
				p.block("attachment_choice", ref.Namespace, ref.Value, "")
				continue
			}
			for _, choice := range choices {
				chosen = choice
			}
		}
		if chosen.State == "linked" {
			if !candidates[chosen.MediaUUID] {
				return models.ErrSourceAttachmentConflict
			}
			if postStates[chosen.MediaUUID] == "unlinked" {
				p.block("attachment_post_unlink", ref.Namespace, ref.Value, chosen.MediaUUID)
			}
		}
		p.preview.Attachments = append(p.preview.Attachments, chosen)
	}
	return nil
}
