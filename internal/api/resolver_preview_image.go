package api

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/previewimage"
)

func (r *sceneResolver) PreviewImage(ctx context.Context, obj *models.Scene) (*PreviewImage, error) {
	baseURL, _ := ctx.Value(BaseURLCtxKey).(string)
	base := fmt.Sprintf("%s/scene/%d/preview-image", baseURL, obj.ID)
	return previewImageModel(base, manager.GetInstance().ScenePreviewImage(obj)), nil
}

func (r *sceneMarkerResolver) PreviewImage(ctx context.Context, obj *models.SceneMarker) (*PreviewImage, error) {
	var scene *models.Scene
	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		var err error
		scene, err = r.repository.Scene.Find(ctx, obj.SceneID)
		return err
	}); err != nil || scene == nil {
		return nil, err
	}
	baseURL, _ := ctx.Value(BaseURLCtxKey).(string)
	base := fmt.Sprintf("%s/scene/%d/scene_marker/%d/preview-image", baseURL, scene.ID, obj.ID)
	return previewImageModel(base, manager.GetInstance().MarkerPreviewImage(scene, obj)), nil
}

func previewImageModel(base string, manifest *previewimage.Manifest) *PreviewImage {
	if manifest == nil {
		return nil
	}
	ret := &PreviewImage{Sources: []*PreviewImageSource{}}
	for _, v := range manifest.Variants {
		url := base + "/" + v.File + "?revision=" + manifest.Revision
		if v.MIMEType == "image/jpeg" {
			ret.Fallback = url
			continue
		}
		ret.Sources = append(ret.Sources, &PreviewImageSource{
			URL: url, MimeType: v.MIMEType, DynamicRange: PreviewImageDynamicRange(v.DynamicRange), Width: v.Width, Height: v.Height,
		})
	}
	if ret.Fallback == "" {
		return nil
	}
	return ret
}
