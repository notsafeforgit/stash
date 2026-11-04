package image

import (
	"context"
	"errors"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// Merge combines two explicitly reviewed image records in a managed write
// transaction. The destination's metadata and primary file win. Files and
// gallery memberships are retained, and historical UUID references follow the
// redirect. Physical duplicate removal is a separate, byte-verified operation.
func Merge(ctx context.Context, repo models.Repository, sourceID, destinationID, sourceRevision, destinationRevision int) (*models.Image, error) {
	if !txn.HasHooks(ctx) {
		return nil, errors.New("image merge requires a managed transaction")
	}
	if sourceID == destinationID || sourceRevision < 1 || destinationRevision < 1 {
		return nil, errors.New("image merge requires distinct images and current revisions")
	}
	identities := make([]*models.ArchiveEntity, 2)
	for i, id := range []int{sourceID, destinationID} {
		identity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveImage, id)
		if err != nil {
			return nil, err
		}
		expected := []int{sourceRevision, destinationRevision}[i]
		if identity == nil || identity.State != models.ArchiveEntityActive || identity.Revision != expected {
			return nil, models.ErrArchiveIdentityConflict
		}
		identities[i] = identity
	}
	source, err := repo.Image.Find(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	destination, err := repo.Image.Find(ctx, destinationID)
	if err != nil {
		return nil, err
	}
	if source == nil || destination == nil {
		return nil, models.ErrArchiveIdentityConflict
	}
	for _, img := range []*models.Image{source, destination} {
		if err := img.LoadFiles(ctx, repo.Image); err != nil {
			return nil, err
		}
		if err := img.LoadGalleryIDs(ctx, repo.Image); err != nil {
			return nil, err
		}
	}
	files := make(map[models.FileID]bool)
	for _, f := range destination.Files.List() {
		files[f.Base().ID] = true
	}
	for _, f := range source.Files.List() {
		if !files[f.Base().ID] {
			if err := repo.Image.AddFileID(ctx, destinationID, f.Base().ID); err != nil {
				return nil, fmt.Errorf("retaining image file: %w", err)
			}
		}
	}
	partial := models.NewImagePartial()
	if destination.PrimaryFileID == nil {
		partial.PrimaryFileID = source.PrimaryFileID
	}
	// Adding existing memberships does not remove/reinsert the destination's
	// memberships, so explicit source exclusions and manual selections survive.
	partial.GalleryIDs = &models.UpdateIDs{Mode: models.RelationshipUpdateModeAdd, IDs: source.GalleryIDs.List()}
	if _, err := repo.Image.UpdatePartial(ctx, destinationID, partial); err != nil {
		return nil, err
	}
	// Covers can follow the redirect now that the survivor is in every gallery.
	// Redirect before deletion also prevents cascades being recorded as manual
	// source-album exclusions. No media or shared generated files are deleted.
	if err := repo.ArchiveEntity.Redirect(ctx, identities[0].UUID, identities[1].UUID, sourceRevision); err != nil {
		return nil, err
	}
	if err := repo.Image.Destroy(ctx, sourceID); err != nil {
		return nil, err
	}
	for _, id := range source.GalleryIDs.List() {
		if _, err := repo.Gallery.UpdatePartial(ctx, id, models.NewGalleryPartial()); err != nil {
			return nil, err
		}
	}
	return repo.Image.Find(ctx, destinationID)
}
