package api

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

func (r *queryResolver) FindSceneMarkers(ctx context.Context, sceneMarkerFilter *models.SceneMarkerFilterType, sceneMarkerFilterAST *models.FilterAST, filter *models.FindFilterType, ids []string) (ret *FindSceneMarkersResultType, err error) {
	idInts, err := handleIDList(ids, "ids")
	if err != nil {
		return nil, err
	}

	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		var sceneMarkers []*models.SceneMarker
		var err error
		var total int

		switch {
		case len(idInts) > 0:
			sceneMarkers, err = r.repository.SceneMarker.FindMany(ctx, idInts)
			total = len(sceneMarkers)
		case sceneMarkerFilterAST != nil:
			sceneMarkers, total, err = r.repository.SceneMarker.QueryAST(ctx, sceneMarkerFilterAST, filter)
		default:
			sceneMarkers, total, err = r.repository.SceneMarker.Query(ctx, sceneMarkerFilter, filter)
		}

		if err != nil {
			return err
		}

		ret = &FindSceneMarkersResultType{
			Count:        total,
			SceneMarkers: sceneMarkers,
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *queryResolver) AllSceneMarkers(ctx context.Context) (ret []*models.SceneMarker, err error) {
	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.SceneMarker.All(ctx)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}
