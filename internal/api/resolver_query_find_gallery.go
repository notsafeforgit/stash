package api

import (
	"context"
	"strconv"

	"github.com/stashapp/stash/pkg/models"
)

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

func (r *queryResolver) FindGalleries(ctx context.Context, galleryFilter *models.GalleryFilterType, galleryFilterAST *models.FilterAST, filter *models.FindFilterType, ids []string) (ret *FindGalleriesResultType, err error) {
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
			galleries, err = r.repository.Gallery.FindMany(ctx, idInts)
			total = len(galleries)
		case galleryFilterAST != nil:
			galleries, total, err = r.repository.Gallery.QueryAST(ctx, galleryFilterAST, filter)
		default:
			galleries, total, err = r.repository.Gallery.Query(ctx, galleryFilter, filter)
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
