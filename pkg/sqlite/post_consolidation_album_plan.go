package sqlite

import (
	"context"
	"database/sql"

	"github.com/stashapp/stash/pkg/models"
)

func (p *postConsolidationPlan) prepareAlbum(ctx context.Context) error {
	if len(p.preview.Blockers) != 0 {
		return nil
	}
	post := p.snapshot.Identity.Destination
	ret := &models.SourceGalleryPreview{PostUUID: post.UUID, PostRevision: post.Revision, Action: "ineligible",
		Entries: []models.SourceAlbumEntry{}, Add: []models.ArchiveEntity{}, Remove: []models.ArchiveEntity{}}
	if choice := p.preview.Gallery; choice != nil {
		ret.Association = &models.SourceGalleryDecision{PostUUID: post.UUID, State: choice.State}
		if choice.State == "linked" {
			id := choice.GalleryUUID
			ret.Association.GalleryUUID = &id
		}
	}
	ready, err := sourceGalleryPreviewReady(ctx, ret)
	if err != nil {
		return err
	}
	if !ready {
		if ret.Action == "review" {
			p.block("gallery_requires_review", "", "", p.preview.Gallery.GalleryUUID)
		}
		return p.setAlbumPlan(ret)
	}
	owners := map[string]bool{}
	for _, member := range p.snapshot.Identity.Members {
		owners[member.UUID] = true
	}
	if ret.Gallery != nil {
		claimed, err := postConsolidationGalleryClaimed(ctx, ret.Gallery.UUID, sortedPostConsolidationKeys(owners))
		if err != nil {
			return err
		}
		if claimed {
			p.block("gallery_claimed", "", "", ret.Gallery.UUID)
			return p.setAlbumPlan(ret)
		}
	}
	if p.selection == nil {
		return p.setAlbumPlan(ret)
	}
	ready, err = sourceGallerySelectPreview(ctx, ret, p.selection, owners)
	if err != nil {
		return err
	}
	if !ready {
		return p.setAlbumPlan(ret)
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
	if err != nil {
		return err
	}
	return p.setAlbumPlan(ret)
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
