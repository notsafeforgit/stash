package gallery

import (
	"context"
	"strconv"

	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/models"
)

// FindCover resolves an explicit image/scene choice first. Without a choice,
// retain the existing cover-filename/first-image behavior, then use the first
// scene for a gallery containing only video. Every lookup stays bounded.
func FindCover(ctx context.Context, repo models.Repository, g *models.Gallery, filenamePattern string) (*models.GalleryCover, error) {
	if g.CoverMediaUUID != nil {
		identity, err := repo.ArchiveEntity.Find(ctx, *g.CoverMediaUUID)
		if err != nil {
			return nil, err
		}
		if identity == nil || identity.State != models.ArchiveEntityActive || identity.LocalID == nil {
			return nil, models.ErrArchiveIdentityConflict
		}
		switch identity.Kind {
		case models.ArchiveImage:
			img, err := repo.Image.Find(ctx, *identity.LocalID)
			return &models.GalleryCover{Image: img}, err
		case models.ArchiveScene:
			scene, err := repo.Scene.Find(ctx, *identity.LocalID)
			return &models.GalleryCover{Scene: scene}, err
		default:
			return nil, models.ErrArchiveIdentityConflict
		}
	}
	img, err := image.FindGalleryCover(ctx, repo.Image, g.ID, filenamePattern)
	if err != nil {
		return nil, err
	}
	if img != nil {
		return &models.GalleryCover{Image: img}, nil
	}
	limit, sortBy, direction := 1, "path", models.SortDirectionEnumAsc
	result, err := repo.Scene.Query(ctx, models.SceneQueryOptions{
		QueryOptions: models.QueryOptions{FindFilter: &models.FindFilterType{PerPage: &limit, Sort: &sortBy, Direction: &direction}},
		SceneFilter: &models.SceneFilterType{Galleries: &models.MultiCriterionInput{
			Value: []string{strconv.Itoa(g.ID)}, Modifier: models.CriterionModifierIncludes,
		}},
	})
	if err != nil {
		return nil, err
	}
	scenes, err := result.Resolve(ctx)
	if err != nil || len(scenes) == 0 {
		return nil, err
	}
	return &models.GalleryCover{Scene: scenes[0]}, nil
}
