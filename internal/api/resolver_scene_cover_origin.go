package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/models"
)

func (r *sceneResolver) CoverOrigin(ctx context.Context, obj *models.Scene) (*SceneCoverOrigin, error) {
	origin, err := manager.GetInstance().SceneCoverOrigin(ctx, obj)
	if err != nil || origin == nil {
		return nil, err
	}
	ret := &SceneCoverOrigin{Status: SceneCoverOriginStatus(origin.Status)}
	if origin.Source != nil {
		id := strconv.Itoa(int(origin.Source.FileID))
		ret.At, ret.SourceFileID = &origin.Source.At, &id
	}
	return ret, nil
}

func (r *mutationResolver) SceneRegenerateCover(ctx context.Context, id string) (string, error) {
	s := manager.GetInstance()
	if !s.Config.GetEnableV3UI() {
		return "", fmt.Errorf("v3 UI is not enabled")
	}
	return strconv.Itoa(s.RegenerateSceneCover(ctx, id)), nil
}
