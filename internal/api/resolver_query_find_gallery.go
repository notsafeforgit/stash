package api

import (
	"context"
	"strconv"

	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
)

func (r *queryResolver) GalleryDeletePreview(ctx context.Context, ids []string) (ret *models.GalleryDeletePreview, err error) {
	keys, err := stringslice.StringSliceToIntSlice(ids)
	if err != nil {
		return nil, err
	}
	err = r.withReadTxn(ctx, func(ctx context.Context) error {
		deletion := gallery.Deletion{Repository: r.repository}
		plan, err := deletion.Plan(ctx, keys)
		if err != nil {
			return err
		}
		ret = &models.GalleryDeletePreview{GalleryCount: len(plan.Galleries), ImageCount: len(plan.Images), SceneCount: len(plan.Scenes),
			SharedImageCount: len(plan.SharedImages), SharedSceneCount: len(plan.SharedScenes), ZipCount: len(plan.ZipFiles)}
		for _, g := range plan.Galleries {
			if g.Origin == models.GalleryOriginSource {
				ret.SourceCount++
			}
		}
		return nil
	})
	return ret, err
}

func (r *queryResolver) FindGallery(ctx context.Context, id string) (ret *models.Gallery, err error) {
	idInt, err := strconv.Atoi(id)
	if err != nil {
		return nil, err
	}

	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.Gallery.Find(ctx, idInt)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *queryResolver) FindGalleries(ctx context.Context, galleryFilterAST *models.FilterAST, filter *models.FindFilterType, ids []string) (ret *FindGalleriesResultType, err error) {
	idInts, err := handleIDList(ids, "ids")
	if err != nil {
		return nil, err
	}

	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		var galleries []*models.Gallery
		var err error
		var total int

		switch {
		case len(idInts) > 0:
			galleries, err = r.repository.Gallery.FindMany(ctx, availableLibraryKeys(idInts))
			total = len(galleries)
		default:
			galleries, total, err = r.repository.Gallery.QueryAST(ctx, galleryFilterAST, filter)
		}

		if err != nil {
			return err
		}

		ret = &FindGalleriesResultType{
			Count:     total,
			Galleries: galleries,
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *queryResolver) AllGalleries(ctx context.Context) (ret []*models.Gallery, err error) {
	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.Gallery.All(ctx)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}
