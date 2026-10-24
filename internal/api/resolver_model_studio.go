package api

import (
	"context"

	"github.com/stashapp/stash/internal/api/loaders"
	"github.com/stashapp/stash/internal/api/urlbuilders"
	"github.com/stashapp/stash/pkg/models"
)

func (r *studioResolver) ImagePath(ctx context.Context, obj *models.Studio) (*string, error) {
	hasImage, err := loaders.From(ctx).StudioCounts.Load(models.EntityCountKey{ID: obj.ID, Field: models.EntityHasImage})
	if err != nil {
		return nil, err
	}

	baseURL, _ := ctx.Value(BaseURLCtxKey).(string)
	publicID, identityErr := r.publicID(ctx, models.ArchiveStudio, obj.ID)
	if identityErr != nil {
		return nil, identityErr
	}
	imagePath := urlbuilders.NewStudioURLBuilder(baseURL, obj, publicID).GetStudioImageURL(hasImage != 0)
	return &imagePath, nil
}

func (r *studioResolver) Aliases(ctx context.Context, obj *models.Studio) ([]string, error) {
	if !obj.Aliases.Loaded() {
		if err := r.withReadTxn(ctx, func(ctx context.Context) error {
			return obj.LoadAliases(ctx, r.repository.Studio)
		}); err != nil {
			return nil, err
		}
	}

	return obj.Aliases.List(), nil
}

func (r *studioResolver) URL(ctx context.Context, obj *models.Studio) (*string, error) {
	if !obj.URLs.Loaded() {
		if err := r.withReadTxn(ctx, func(ctx context.Context) error {
			return obj.LoadURLs(ctx, r.repository.Studio)
		}); err != nil {
			return nil, err
		}
	}

	urls := obj.URLs.List()
	if len(urls) == 0 {
		return nil, nil
	}

	return &urls[0], nil
}

func (r *studioResolver) Urls(ctx context.Context, obj *models.Studio) ([]string, error) {
	if !obj.URLs.Loaded() {
		if err := r.withReadTxn(ctx, func(ctx context.Context) error {
			return obj.LoadURLs(ctx, r.repository.Studio)
		}); err != nil {
			return nil, err
		}
	}

	return obj.URLs.List(), nil
}

func (r *studioResolver) Tags(ctx context.Context, obj *models.Studio) (ret []*models.Tag, err error) {
	if !obj.TagIDs.Loaded() {
		if err := r.withReadTxn(ctx, func(ctx context.Context) error {
			return obj.LoadTagIDs(ctx, r.repository.Studio)
		}); err != nil {
			return nil, err
		}
	}

	var errs []error
	ret, errs = loaders.From(ctx).TagByID.LoadAll(obj.TagIDs.List())
	return ret, firstError(errs)
}

func (r *studioResolver) SceneCount(ctx context.Context, obj *models.Studio, depth *int) (int, error) {
	d := 0
	if depth != nil {
		d = *depth
	}
	return loaders.From(ctx).StudioCounts.Load(models.EntityCountKey{ID: obj.ID, Depth: d, Field: models.EntityCountScenes})
}

func (r *studioResolver) ImageCount(ctx context.Context, obj *models.Studio, depth *int) (int, error) {
	d := 0
	if depth != nil {
		d = *depth
	}
	return loaders.From(ctx).StudioCounts.Load(models.EntityCountKey{ID: obj.ID, Depth: d, Field: models.EntityCountImages})
}

func (r *studioResolver) GalleryCount(ctx context.Context, obj *models.Studio, depth *int) (int, error) {
	d := 0
	if depth != nil {
		d = *depth
	}
	return loaders.From(ctx).StudioCounts.Load(models.EntityCountKey{ID: obj.ID, Depth: d, Field: models.EntityCountGalleries})
}

func (r *studioResolver) PerformerCount(ctx context.Context, obj *models.Studio, depth *int) (int, error) {
	d := 0
	if depth != nil {
		d = *depth
	}
	return loaders.From(ctx).StudioCounts.Load(models.EntityCountKey{ID: obj.ID, Depth: d, Field: models.EntityCountPerformers})
}

func (r *studioResolver) GroupCount(ctx context.Context, obj *models.Studio, depth *int) (int, error) {
	d := 0
	if depth != nil {
		d = *depth
	}
	return loaders.From(ctx).StudioCounts.Load(models.EntityCountKey{ID: obj.ID, Depth: d, Field: models.EntityCountGroups})
}

func (r *studioResolver) SceneMarkerCount(ctx context.Context, obj *models.Studio, depth *int) (int, error) {
	d := 0
	if depth != nil {
		d = *depth
	}
	return loaders.From(ctx).StudioCounts.Load(models.EntityCountKey{ID: obj.ID, Depth: d, Field: models.EntityCountMarkers})
}

func (r *studioResolver) OCounter(ctx context.Context, obj *models.Studio, depth *int) (int, error) {
	d := 0
	if depth != nil {
		d = *depth
	}
	return loaders.From(ctx).StudioCounts.Load(models.EntityCountKey{ID: obj.ID, Depth: d, Field: models.EntityCountO})
}

func (r *studioResolver) ParentStudio(ctx context.Context, obj *models.Studio) (ret *models.Studio, err error) {
	if obj.ParentID == nil {
		return nil, nil
	}

	return loaders.From(ctx).StudioByID.Load(*obj.ParentID)
}

func (r *studioResolver) ChildStudios(ctx context.Context, obj *models.Studio) (ret []*models.Studio, err error) {
	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.Studio.FindChildren(ctx, obj.ID)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *studioResolver) StashIds(ctx context.Context, obj *models.Studio) ([]*models.StashID, error) {
	if !obj.StashIDs.Loaded() {
		if err := r.withReadTxn(ctx, func(ctx context.Context) error {
			return obj.LoadStashIDs(ctx, r.repository.Studio)
		}); err != nil {
			return nil, err
		}
	}

	return stashIDsSliceToPtrSlice(obj.StashIDs.List()), nil
}

func (r *studioResolver) Rating100(ctx context.Context, obj *models.Studio) (*int, error) {
	return obj.Rating, nil
}

func (r *studioResolver) Groups(ctx context.Context, obj *models.Studio) (ret []*models.Group, err error) {
	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.Group.FindByStudioID(ctx, obj.ID)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *studioResolver) CustomFields(ctx context.Context, obj *models.Studio) (map[string]interface{}, error) {
	m, err := loaders.From(ctx).StudioCustomFields.Load(obj.ID)
	if err != nil {
		return nil, err
	}

	if m == nil {
		return make(map[string]interface{}), nil
	}

	return m, nil
}
