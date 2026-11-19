package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/stashapp/stash/pkg/models"
)

func (p *postConsolidationPlan) prepareAlbum(ctx context.Context) error {
	if len(p.preview.Blockers) != 0 {
		return nil
	}
	ret, ready, err := p.mergedPostAlbum(ctx, nil, false)
	if err != nil {
		return err
	}
	if !ready {
		if ret.Action == "review" {
			p.block("gallery_requires_review", "", "", p.preview.Gallery.GalleryUUID)
		}
		return p.setAlbumPlan(ret)
	}
	// After the merge, the ordinary preview of a post in a self-reply thread is
	// the thread album, whichever merged post carried the thread. A reviewed
	// merge concerns the duplicate posts, not their neighbouring replies, so it
	// leaves that album to the independent thread operation and makes no
	// gallery change. Predict the post-merge preview the same way to decide.
	destination := p.snapshot.Identity.Destination.UUID
	members, err := p.mergedThreadMembers(ctx)
	if errors.Is(err, models.ErrSourceGalleryConflict) || errors.Is(err, models.ErrSourceAlbumLimit) {
		// A thread that needs review keeps the single-post plan; the merge
		// applies only when that plan makes no gallery change.
		return p.setAlbumPlan(ret)
	}
	if err != nil {
		return err
	}
	if len(members) > 1 {
		gallery := &SourceGalleryStore{}
		thread, err := gallery.combineThread(ctx, ret, members, func(ctx context.Context, post string, shared *models.ArchiveEntity, eligibility bool) (*models.SourceGalleryPreview, error) {
			if post == destination {
				merged, _, err := p.mergedPostAlbum(ctx, shared, true)
				return merged, err
			}
			return gallery.previewSinglePost(ctx, post, nil, shared, true)
		})
		if err != nil {
			return err
		}
		if thread, err = sourceGalleryLocalMinimum(thread); err != nil {
			return err
		}
		if len(thread.ThreadPlans) > 0 {
			post := p.snapshot.Identity.Destination
			deferred, err := finishSourceGalleryPreview(&models.SourceGalleryPreview{PostUUID: post.UUID, PostRevision: post.Revision,
				Action: "ineligible", Entries: []models.SourceAlbumEntry{}, Add: []models.ArchiveEntity{}, Remove: []models.ArchiveEntity{}})
			if err != nil {
				return err
			}
			return p.setAlbumPlan(deferred)
		}
	}
	return p.setAlbumPlan(ret)
}

// mergedThreadMembers returns the thread members as sourceThreadMembers will
// read them after the merge: the merged identity carries the thread row of
// either post, both rows conflict, and the source's place belongs to the
// destination.
func (p *postConsolidationPlan) mergedThreadMembers(ctx context.Context) ([]sourceThreadRow, error) {
	source, destination := p.snapshot.Identity.Source.UUID, p.snapshot.Identity.Destination.UUID
	sourceRow, err := sourceThreadForPost(ctx, source)
	if err != nil {
		return nil, err
	}
	destinationRow, err := sourceThreadForPost(ctx, destination)
	if err != nil {
		return nil, err
	}
	var members []sourceThreadRow
	switch {
	case sourceRow != nil && destinationRow != nil:
		return nil, models.ErrSourceGalleryConflict
	case destinationRow != nil:
		members, err = sourceThreadMembers(ctx, destination)
	case sourceRow != nil:
		members, err = sourceThreadMembers(ctx, source)
	}
	for i := range members {
		if members[i].PostUUID == source {
			members[i].PostUUID = destination
		}
	}
	return members, err
}

// mergedPostAlbum previews the destination's own album with the merge's
// resulting selection, gallery and media choices, as previewSinglePost would
// read them once the merge is published. ready is false when the gallery
// association prevents album changes.
func (p *postConsolidationPlan) mergedPostAlbum(ctx context.Context, shared *models.ArchiveEntity, forceAlbum bool) (*models.SourceGalleryPreview, bool, error) {
	post := p.snapshot.Identity.Destination
	ret := &models.SourceGalleryPreview{PostUUID: post.UUID, PostRevision: post.Revision, Action: "ineligible",
		Entries: []models.SourceAlbumEntry{}, Add: []models.ArchiveEntity{}, Remove: []models.ArchiveEntity{}}
	if choice := p.preview.Gallery; choice != nil {
		// publishGalleryChoice records the merged choice as a review decision.
		ret.Association = &models.SourceGalleryDecision{PostUUID: post.UUID, State: choice.State, Origin: "review"}
		if choice.State == "linked" {
			id := choice.GalleryUUID
			ret.Association.GalleryUUID = &id
		}
	}
	ready, err := sourceGalleryPreviewReady(ctx, ret)
	if err != nil || !ready {
		return ret, false, err
	}
	if shared != nil && ret.Gallery == nil {
		ret.Gallery = shared
	}
	owners := map[string]bool{}
	for _, member := range p.snapshot.Identity.Members {
		owners[member.UUID] = true
	}
	// The selected gallery already belongs to a member of this merge. Other
	// posts may share it; the ordinary membership preview preserves their media.
	if p.selection == nil {
		ret, err = finishSourceGalleryPreview(ret)
		return ret, true, err
	}
	selected, err := sourceGallerySelectPreview(ctx, ret, p.selection, owners, forceAlbum)
	if err != nil {
		return nil, true, err
	}
	if !selected {
		ret, err = finishSourceGalleryPreview(ret)
		return ret, true, err
	}
	byReference := map[models.SourcePostIdentifier]models.PostConsolidationAttachmentChoice{}
	for _, choice := range p.preview.Attachments {
		byReference[models.SourcePostIdentifier{Namespace: choice.Namespace, Value: choice.Value}] = choice
	}
	choices := map[string]sourceAlbumMediaChoice{}
	for _, entry := range p.selection.Entries {
		if chosen, exists := byReference[entry.Attachment.Reference]; exists {
			choices[entry.Attachment.UUID] = sourceAlbumMediaChoice{AttachmentUUID: entry.Attachment.UUID, State: chosen.State,
				MediaUUID: sql.NullString{String: chosen.MediaUUID, Valid: chosen.State == "linked"}}
		}
	}
	postChoices := make([]sourcePostMediaRow, 0, len(p.preview.Media))
	for _, choice := range p.preview.Media {
		postChoices = append(postChoices, sourcePostMediaRow{PostUUID: post.UUID, MediaUUID: choice.MediaUUID, State: choice.State})
	}
	ret, err = sourceGalleryMembershipPreview(ctx, ret, p.selection, choices, postChoices, owners)
	return ret, true, err
}

func postConsolidationGalleryClaimed(ctx context.Context, gallery string, owners []string) (bool, error) {
	var aliases []string
	if err := dbWrapper.Select(ctx, &aliases, sourceAlbumGalleryAliasesQuery, gallery); err != nil {
		return false, err
	}
	if len(aliases) > 1024 {
		return false, models.ErrSourceAlbumLimit
	}
	query, args := postConsolidationGalleryClaimQuery(aliases, owners)
	var claimed bool
	err := dbWrapper.Get(ctx, &claimed, query, args...)
	return claimed, err
}

func postConsolidationGalleryClaimQuery(aliases, owners []string) (string, []any) {
	args := make([]any, 0, len(aliases)+len(owners))
	for _, id := range aliases {
		args = append(args, id)
	}
	for _, id := range owners {
		args = append(args, id)
	}
	return `SELECT EXISTS(SELECT 1 FROM post_gallery_links INDEXED BY post_gallery_links_gallery
WHERE gallery_uuid IN ` + getInBinding(len(aliases)) + ` AND post_uuid NOT IN ` + getInBinding(len(owners)) + `)`, args
}

func (p *postConsolidationPlan) setAlbumPlan(preview *models.SourceGalleryPreview) error {
	var err error
	preview, err = sourceGalleryLocalMinimum(preview)
	if err != nil {
		return err
	}
	p.gallery, err = finishSourceGalleryPreview(preview)
	if err != nil {
		return err
	}
	plan := &models.PostConsolidationAlbumPlan{Action: preview.Action, Title: preview.Title, Details: preview.Details,
		Entries: []models.PostConsolidationAlbumEntry{}, Add: []models.SourcePostComparisonEntity{}, Remove: []models.SourcePostComparisonEntity{}}
	if preview.Gallery != nil {
		plan.GalleryUUID = preview.Gallery.UUID
	}
	if preview.Date != nil {
		plan.Date = preview.Date.String()
	}
	refs := map[string]models.SourcePostIdentifier{}
	if p.selection != nil {
		for _, entry := range p.selection.Entries {
			refs[entry.Attachment.UUID] = entry.Attachment.Reference
		}
	}
	for _, entry := range preview.Entries {
		ref := refs[entry.AttachmentUUID]
		item := models.PostConsolidationAlbumEntry{Position: entry.Position, Namespace: ref.Namespace, Value: ref.Value, Status: entry.Status}
		if entry.MediaUUID != nil {
			item.MediaUUID = *entry.MediaUUID
		}
		plan.Entries = append(plan.Entries, item)
	}
	for _, entity := range preview.Add {
		plan.Add = append(plan.Add, comparisonEntity(&entity))
	}
	for _, entity := range preview.Remove {
		plan.Remove = append(plan.Remove, comparisonEntity(&entity))
	}
	p.preview.Album = plan
	return nil
}
