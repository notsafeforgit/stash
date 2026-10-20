// Package bulkupdate owns the domain operations used by immediate and durable
// bulk edits. HTTP input translation and job execution share these operations.
package bulkupdate

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/group"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/performer"
	"github.com/stashapp/stash/pkg/scene"
	"github.com/stashapp/stash/pkg/studio"
	"github.com/stashapp/stash/pkg/tag"
)

type GalleryUpdater interface {
	ValidateImageGalleryChange(context.Context, *models.Image, models.UpdateIDs) error
	Updated(context.Context, int) error
}

type GroupUpdater interface {
	UpdatePartial(context.Context, int, models.GroupPartial, group.ImageInput, group.ImageInput) (*models.Group, error)
}

type Scene struct {
	Repository   models.SceneReaderWriter
	Changes      models.ScenePartial
	CustomFields *models.CustomFieldsInput
}

func (o Scene) Update(ctx context.Context, id int) error {
	scene, err := o.Repository.UpdatePartial(ctx, id, o.Changes)
	if err != nil {
		return err
	}

	if o.CustomFields != nil {
		if err := o.Repository.SetCustomFields(ctx, scene.ID, *o.CustomFields); err != nil {
			return err
		}
	}

	return nil
}

type SceneMarker struct {
	Repository models.SceneMarkerReaderWriter
	Changes    models.SceneMarkerPartial
}

func (o SceneMarker) Update(ctx context.Context, id int) error {
	changes := o.Changes
	if err := scene.AdjustMarkerTags(ctx, o.Repository, id, &changes); err != nil {
		return err
	}

	_, err := o.Repository.UpdatePartial(ctx, id, changes)
	return err
}

type SceneDateFromMTime struct {
	Repository models.SceneReaderWriter
	Files      models.FileGetter
}

func (o SceneDateFromMTime) Update(ctx context.Context, id int) error {
	s, err := o.Repository.Find(ctx, id)
	if err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("scene with id %d not found", id)
	}

	if err := s.LoadPrimaryFile(ctx, o.Files); err != nil {
		return fmt.Errorf("loading primary file: %w", err)
	}
	primary := s.Files.Primary()
	if primary == nil {
		// no primary file — nothing to derive from; skip silently so
		// bulk runs don't abort on isolated cases.
		return nil
	}

	mtime := primary.Base().ModTime
	partial := models.NewScenePartial()
	partial.Date = models.NewOptionalDate(models.Date{Time: mtime, Precision: models.DatePrecisionDay})

	if _, err := o.Repository.UpdatePartial(ctx, id, partial); err != nil {
		return err
	}
	return nil
}

type Image struct {
	Repository models.ImageReaderWriter
	Galleries  GalleryUpdater
	Changes    models.ImagePartial
}

func (o Image) Update(ctx context.Context, id int) error {
	i, err := o.Repository.Find(ctx, id)
	if err != nil {
		return err
	}
	if i == nil {
		return fmt.Errorf("image with id %d not found", id)
	}

	if o.Changes.GalleryIDs != nil {
		if err := i.LoadGalleryIDs(ctx, o.Repository); err != nil {
			return err
		}

		if err := o.Galleries.ValidateImageGalleryChange(ctx, i, *o.Changes.GalleryIDs); err != nil {
			return err
		}
	}

	if _, err := o.Repository.UpdatePartial(ctx, id, o.Changes); err != nil {
		return err
	}

	if o.Changes.GalleryIDs != nil {
		thisUpdatedGalleryIDs := o.Changes.GalleryIDs.ImpactedIDs(i.GalleryIDs.List())
		for _, galleryID := range thisUpdatedGalleryIDs {
			if err := o.Galleries.Updated(ctx, galleryID); err != nil {
				return fmt.Errorf("updating gallery %d: %w", galleryID, err)
			}
		}
	}

	return nil
}

type ImageDateFromMTime struct {
	Repository models.ImageReaderWriter
	Files      models.FileGetter
}

func (o ImageDateFromMTime) Update(ctx context.Context, id int) error {
	i, err := o.Repository.Find(ctx, id)
	if err != nil {
		return err
	}
	if i == nil {
		return fmt.Errorf("image with id %d not found", id)
	}

	if err := i.LoadPrimaryFile(ctx, o.Files); err != nil {
		return fmt.Errorf("loading primary file: %w", err)
	}
	primary := i.Files.Primary()
	if primary == nil {
		return nil
	}

	mtime := primary.Base().ModTime
	partial := models.NewImagePartial()
	partial.Date = models.NewOptionalDate(models.Date{Time: mtime, Precision: models.DatePrecisionDay})

	if _, err := o.Repository.UpdatePartial(ctx, id, partial); err != nil {
		return err
	}
	return nil
}

type Performer struct {
	Repository models.PerformerReaderWriter
	Changes    models.PerformerPartial
	URLs       performer.URLUpdate
}

func (o Performer) Update(ctx context.Context, id int) error {
	if o.URLs.AnySet() {
		if err := performer.ApplyUpdateURLs(ctx, o.Repository, id, o.URLs, &o.Changes); err != nil {
			return err
		}
	}

	if err := performer.ValidateUpdate(ctx, id, o.Changes, o.Repository); err != nil {
		return err
	}

	_, err := o.Repository.UpdatePartial(ctx, id, o.Changes)
	return err
}

type Gallery struct {
	Repository models.GalleryReaderWriter
	Changes    models.GalleryPartial
}

func (o Gallery) Update(ctx context.Context, id int) error {
	_, err := o.Repository.UpdatePartial(ctx, id, o.Changes)
	return err
}

type Studio struct {
	Repository models.StudioReaderWriter
	Changes    models.StudioPartial
}

func (o Studio) Update(ctx context.Context, id int) error {
	local := o.Changes
	local.ID = id
	if err := studio.ValidateModify(ctx, local, o.Repository); err != nil {
		return err
	}

	_, err := o.Repository.UpdatePartial(ctx, local)
	return err
}

type Tag struct {
	Repository models.TagReaderWriter
	Changes    models.TagPartial
}

func (o Tag) Update(ctx context.Context, id int) error {
	if err := tag.ValidateUpdate(ctx, id, o.Changes, o.Repository); err != nil {
		return err
	}

	_, err := o.Repository.UpdatePartial(ctx, id, o.Changes)
	return err
}

type Group struct {
	Groups  GroupUpdater
	Changes models.GroupPartial
}

func (o Group) Update(ctx context.Context, id int) error {
	_, err := o.Groups.UpdatePartial(ctx, id, o.Changes, group.ImageInput{}, group.ImageInput{})
	return err
}
