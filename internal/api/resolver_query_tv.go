package api

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

func (r *queryResolver) FindTVMarkerFeed(ctx context.Context, markerFilterAST *models.FilterAST, sceneFilter TVSceneFilterInput, filter *models.FindFilterType) (*TVMarkerFeedResult, error) {
	var ret TVMarkerFeedResult
	err := r.withReadTxn(ctx, func(ctx context.Context) error {
		var err error
		ret.Items, ret.Count, err = r.repository.SceneMarker.QueryTV(ctx, markerFilterAST, sceneFilter.FilterAst, filter, sceneFilter.Search)
		return err
	})
	return &ret, err
}
