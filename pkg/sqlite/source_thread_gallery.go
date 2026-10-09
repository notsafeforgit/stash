package sqlite

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

func (s *SourceGalleryStore) previewThread(ctx context.Context, value string, proposed map[string]sourceAlbumMediaChoice, members []sourceThreadRow) (*models.SourceGalleryPreview, error) {
	current, err := s.previewSinglePost(ctx, value, proposed, nil, false)
	if err != nil {
		return nil, err
	}
	if current.Action == "disabled" || current.Action == "review" || (current.Association != nil && current.Association.Origin != "source") {
		return current, nil
	}
	var shared *models.ArchiveEntity
	var eligible []sourceThreadRow
	for _, member := range members {
		p, err := s.previewSinglePost(ctx, member.PostUUID, nil, nil, true)
		if err != nil {
			return nil, err
		}
		if p.Action == "review" {
			current.Action = "review"
			return finishSourceGalleryPreview(current)
		}
		if p.Action == "disabled" && p.Association != nil && p.Association.State == "linked" {
			current.Action = "disabled"
			return finishSourceGalleryPreview(current)
		}
		if p.Action == "disabled" || p.SelectionUUID == "" || (p.Association != nil && p.Association.Origin != "source") {
			continue
		}
		if p.Gallery != nil {
			if shared != nil && shared.UUID != p.Gallery.UUID {
				current.Action = "review"
				return finishSourceGalleryPreview(current)
			}
			shared = p.Gallery
		}
		eligible = append(eligible, member)
	}
	if len(eligible) < 2 {
		return current, nil
	}
	ret := *current
	ret.Gallery, ret.Add, ret.Remove, ret.Entries = shared, []models.ArchiveEntity{}, []models.ArchiveEntity{}, []models.SourceAlbumEntry{}
	ret.Action = "create"
	if shared != nil {
		ret.Action = "sync"
	}
	adds, removes, wanted := map[string]models.ArchiveEntity{}, map[string]models.ArchiveEntity{}, map[string]bool{}
	var signatures []string
	offset := 0
	for _, member := range eligible {
		var choices map[string]sourceAlbumMediaChoice
		if member.PostUUID == current.PostUUID {
			choices = proposed
		}
		p, err := s.previewSinglePost(ctx, member.PostUUID, choices, shared, true)
		if err != nil {
			return nil, err
		}
		ret.ThreadPlans = append(ret.ThreadPlans, p)
		signatures = append(signatures, p.Signature)
		for _, e := range p.Entries {
			if e.MediaUUID != nil && e.Status == "linked" {
				wanted[*e.MediaUUID] = true
			}
			e.Position += offset
			if e.Position >= archive.MaxManifestPositions {
				return nil, models.ErrSourceAlbumLimit
			}
			ret.Entries = append(ret.Entries, e)
		}
		if len(p.Entries) > 0 {
			offset = ret.Entries[len(ret.Entries)-1].Position + 1
		}
		for _, m := range p.Add {
			adds[m.UUID] = m
		}
		for _, m := range p.Remove {
			removes[m.UUID] = m
		}
		if len(ret.Entries) > maxSourceGalleryMembers {
			return nil, models.ErrSourceAlbumLimit
		}
	}
	// A shared image can belong to several replies. Removing one post's link
	// must not remove media still selected by another reply.
	for _, p := range ret.ThreadPlans {
		p.Remove = slices.DeleteFunc(p.Remove, func(m models.ArchiveEntity) bool { return wanted[m.UUID] })
	}
	for _, m := range adds {
		ret.Add = append(ret.Add, m)
	}
	for id, m := range removes {
		if !wanted[id] {
			ret.Remove = append(ret.Remove, m)
		}
	}
	for _, list := range [][]models.ArchiveEntity{ret.Add, ret.Remove} {
		slices.SortFunc(list, func(a, b models.ArchiveEntity) int { return cmp.Compare(a.UUID, b.UUID) })
	}
	if shared == nil {
		first := ret.ThreadPlans[0]
		ret.Title, ret.Details, ret.Date = first.Title, first.Details, first.Date
		if strings.TrimSpace(first.Details) != "" {
			ret.Title = string([]rune(strings.TrimSpace(first.Details))[:min(200, len([]rune(strings.TrimSpace(first.Details))))])
		}
		if ret.Title == "" || ret.Title == "Source album" {
			ret.Title = sourceThreadFallbackTitle(eligible[0])
		}
	}
	if _, err := finishSourceGalleryPreview(&ret); err != nil {
		return nil, err
	}
	ret.Signature, err = sourceSignature("stash-thread-gallery-preview-v1", []any{ret.Signature, signatures, eligible})
	return &ret, err
}

func (s *SourceGalleryStore) syncThread(ctx context.Context, preview *models.SourceGalleryPreview) (*models.SourceGallerySyncResult, error) {
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrSourceGalleryConflict
		}
		return nil
	})
	ret := &models.SourceGallerySyncResult{Action: preview.Action, Added: []string{}, Removed: []string{}}
	shared := preview.Gallery
	if shared != nil {
		ret.GalleryUUID, ret.GalleryID = shared.UUID, shared.LocalID
	}
	added, removed := map[string]bool{}, map[string]bool{}
	for _, original := range preview.ThreadPlans {
		p := *original
		if shared != nil {
			var err error
			shared, err = (&ArchiveEntityStore{}).Find(ctx, shared.UUID)
			if err != nil {
				return nil, err
			}
			p.Gallery, p.Action = shared, "sync"
		} else {
			p.Title, p.Details, p.Date = preview.Title, preview.Details, preview.Date
		}
		p.Add = slices.DeleteFunc(slices.Clone(p.Add), func(m models.ArchiveEntity) bool { return added[m.UUID] })
		p.Remove = slices.DeleteFunc(slices.Clone(p.Remove), func(m models.ArchiveEntity) bool { return removed[m.UUID] })
		result, err := s.syncPreview(ctx, &p)
		if err != nil {
			return nil, err
		}
		if shared == nil {
			shared, err = (&ArchiveEntityStore{}).Find(ctx, result.GalleryUUID)
			if err != nil {
				return nil, err
			}
		}
		ret.GalleryUUID, ret.GalleryID = shared.UUID, shared.LocalID
		ret.Created = ret.Created || result.Created
		for _, id := range result.Added {
			if !added[id] {
				ret.Added = append(ret.Added, id)
				added[id] = true
			}
		}
		for _, id := range result.Removed {
			if !removed[id] {
				ret.Removed = append(ret.Removed, id)
				removed[id] = true
			}
		}
	}
	final, err := s.Preview(ctx, preview.PostUUID)
	if err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		current, err := s.Preview(ctx, preview.PostUUID)
		if err != nil {
			return err
		}
		if current.Signature != final.Signature {
			return models.ErrSourceGalleryConflict
		}
		return nil
	})
	complete = true
	return ret, nil
}
