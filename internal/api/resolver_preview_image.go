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
	publicID, err := r.publicID(ctx, models.ArchiveScene, obj.ID)
	if err != nil {
		return nil, err
	}
	base := fmt.Sprintf("%s/scene/%s/preview-image", baseURL, publicID)
	return previewImageModel(base, manager.GetInstance().ScenePreviewImage(obj)), nil
}

func (r *imageResolver) PreviewImage(ctx context.Context, obj *models.Image) (*PreviewImage, error) {
	baseURL, _ := ctx.Value(BaseURLCtxKey).(string)
	publicID, err := r.publicID(ctx, models.ArchiveImage, obj.ID)
	if err != nil {
		return nil, err
	}
	base := fmt.Sprintf("%s/image/%s/preview-image", baseURL, publicID)
	return previewImageModel(base, manager.GetInstance().ImagePreviewImage(obj)), nil
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
	sceneID, err := r.publicID(ctx, models.ArchiveScene, scene.ID)
	if err != nil {
		return nil, err
	}
	markerID, err := r.publicID(ctx, models.ArchiveSceneMarker, obj.ID)
	if err != nil {
		return nil, err
	}
	base := fmt.Sprintf("%s/scene/%s/scene_marker/%s/preview-image", baseURL, sceneID, markerID)
	return previewImageModel(base, manager.GetInstance().MarkerPreviewImage(scene, obj)), nil
}

func previewImageModel(base string, manifest *previewimage.Manifest) *PreviewImage {
	if manifest == nil {
		return nil
	}
	ret := previewImageRenditionModel(base, manifest.Revision, manifest.Variants)
	if ret != nil {
		ret.Thumbnail = previewImageRenditionModel(base, manifest.Revision, manifest.Thumbnail)
	}
	return ret
}

func previewImageRenditionModel(base, revision string, variants []previewimage.Variant) *PreviewImage {
	ret := &PreviewImage{Sources: []*PreviewImageSource{}}
	for _, v := range variants {
		url := base + "/" + v.File + "?revision=" + revision
		if v.MIMEType == "image/jpeg" {
			ret.Fallback = url
			continue
		}
		ret.Sources = append(ret.Sources, &PreviewImageSource{
			URL: url, MimeType: v.MIMEType, DynamicRange: PreviewImageDynamicRange(v.DynamicRange), Width: v.Width, Height: v.Height,
		})
	}
	if ret.Fallback == "" {
		for _, source := range ret.Sources {
			if source.DynamicRange != PreviewImageDynamicRangeHdr {
				ret.Fallback = source.URL
				break
			}
		}
		if ret.Fallback == "" {
			return nil
		}
	}
	return ret
}
