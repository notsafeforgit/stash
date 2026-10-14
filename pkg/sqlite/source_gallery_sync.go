package sqlite

import (
	"context"
	"errors"
	"slices"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

func (s *SourceGalleryStore) Sync(ctx context.Context, post, signature string) (*models.SourceGallerySyncResult, error) {
	preview, err := s.Preview(ctx, post)
	if err != nil {
		return nil, err
	}
	if signature == "" || signature != preview.Signature || preview.Action == "review" {
		return nil, models.ErrSourceGalleryConflict
	}
	var ret *models.SourceGallerySyncResult
	if len(preview.ThreadPlans) > 0 {
		ret, err = s.syncThread(ctx, preview)
	} else {
		ret, err = s.syncPreview(ctx, preview)
	}
	if err != nil || ret.GalleryUUID == "" || (ret.Action != "create" && ret.Action != "sync") {
		return ret, err
	}
	merge, err := s.PreviewConsolidation(ctx, ret.GalleryUUID)
	if err != nil && !errors.Is(err, models.ErrSourceAlbumLimit) {
		return nil, err
	}
	// An oversized component needs separate review, but thread synchronization
	// must still register its transaction guard below.
	if err == nil && len(merge.Galleries) > 1 && len(merge.Conflicts) == 0 {
		result, err := s.consolidate(ctx, merge)
		if err != nil {
			return nil, err
		}
		if ret.GalleryUUID != result.GalleryUUID {
			ret.Created, ret.Action = false, "sync"
		}
		ret.GalleryUUID = result.GalleryUUID
		id := merge.Galleries[0].LocalID
		ret.GalleryID = &id
		for _, id := range result.Added {
			if !slices.Contains(ret.Added, id) {
				ret.Added = append(ret.Added, id)
			}
		}
	}
	if len(preview.ThreadPlans) == 0 {
		return ret, nil
	}
	final, err := s.Preview(ctx, post)
	if err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		current, err := s.Preview(ctx, post)
		if err != nil {
			return err
		}
		if current.Signature != final.Signature {
			return models.ErrSourceGalleryConflict
		}
		return nil
	})
	return ret, nil
}

func (s *SourceGalleryStore) syncPreview(ctx context.Context, preview *models.SourceGalleryPreview) (*models.SourceGallerySyncResult, error) {
	ret := &models.SourceGallerySyncResult{Action: preview.Action, Added: []string{}, Removed: []string{}}
	if preview.Gallery != nil {
		ret.GalleryUUID, ret.GalleryID = preview.Gallery.UUID, preview.Gallery.LocalID
	}
	if preview.Action != "create" && preview.Action != "sync" {
		return ret, nil
	}
	if preview.Action == "create" {
		gallery := models.NewGallery()
		gallery.Origin, gallery.Title, gallery.Details, gallery.Date = models.GalleryOriginSource, preview.Title, preview.Details, preview.Date
		if err := s.gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &gallery}); err != nil {
			return nil, err
		}
		identity, err := (&ArchiveEntityStore{}).FindByLocalID(ctx, models.ArchiveGallery, gallery.ID)
		if err != nil {
			return nil, err
		}
		if identity == nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
		if err := recordNewSourceGalleryMetadata(ctx, identity.UUID, preview); err != nil {
			return nil, err
		}
		identity, err = (&ArchiveEntityStore{}).Find(ctx, identity.UUID)
		if err != nil {
			return nil, err
		}
		if _, err := s.decideAssociation(ctx, models.SourceGalleryChoiceInput{PostUUID: preview.PostUUID, ExpectedPostRevision: preview.PostRevision,
			State: "linked", GalleryUUID: identity.UUID, ExpectedGalleryRevision: identity.Revision, Origin: "source", Reason: "Created from selected post attachments"}, &preview.SelectionUUID); err != nil {
			return nil, err
		}
		ret.GalleryUUID, ret.GalleryID, ret.Created = identity.UUID, identity.LocalID, true
	}
	if preview.Action == "sync" && preview.Association == nil {
		if _, err := s.decideAssociation(ctx, models.SourceGalleryChoiceInput{PostUUID: preview.PostUUID, ExpectedPostRevision: preview.PostRevision,
			State: "linked", GalleryUUID: preview.Gallery.UUID, ExpectedGalleryRevision: preview.Gallery.Revision, Origin: "source", Reason: "Shared gallery for captured self-replies"}, &preview.SelectionUUID); err != nil {
			return nil, err
		}
	}
	if len(preview.Add) == 0 && len(preview.Remove) == 0 {
		return ret, nil
	}
	err := withSourceGalleryWrite(ctx, ret.GalleryUUID, preview.PostUUID, preview.SelectionUUID, func() error {
		for _, media := range preview.Remove {
			if media.Kind == models.ArchiveImage {
				if err := s.gallery.RemoveImages(ctx, *ret.GalleryID, *media.LocalID); err != nil {
					return err
				}
			} else {
				if _, err := dbWrapper.Exec(ctx, "DELETE FROM scenes_galleries WHERE gallery_id = ? AND scene_id = ?", *ret.GalleryID, *media.LocalID); err != nil {
					return err
				}
			}
			ret.Removed = append(ret.Removed, media.UUID)
		}
		for _, media := range preview.Add {
			if media.Kind == models.ArchiveImage {
				if err := s.gallery.AddImages(ctx, *ret.GalleryID, *media.LocalID); err != nil {
					return err
				}
			} else {
				if err := s.gallery.AddSceneIDs(ctx, *ret.GalleryID, []int{*media.LocalID}); err != nil {
					return err
				}
			}
			ret.Added = append(ret.Added, media.UUID)
		}
		_, err := s.gallery.UpdatePartial(ctx, *ret.GalleryID, models.NewGalleryPartial())
		return err
	})
	if err != nil {
		return nil, err
	}
	return ret, nil
}
