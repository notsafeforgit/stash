package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
)

// Fork bulk operations. Legacy resolvers delegate here with explicit IDs;
// only the additive job mutations may select items by filter.

type galleryBulkUpdateOperation struct {
	repository     models.GalleryReaderWriter
	updatedGallery models.GalleryPartial
}

func (o galleryBulkUpdateOperation) Update(ctx context.Context, id int) error {
	_, err := o.repository.UpdatePartial(ctx, id, o.updatedGallery)
	return err
}

func (r *mutationResolver) BulkGalleryUpdateJob(ctx context.Context, input BulkGalleryUpdateInput) (string, error) {
	galleryIDs, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return "", fmt.Errorf("converting ids: %w", err)
	}

	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.GalleryFilterAst) {
			return "", fmt.Errorf("gallery_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		err = r.withReadTxn(ctx, func(ctx context.Context) error {
			result, _, qErr := r.repository.Gallery.QueryAST(ctx, input.GalleryFilterAst, findFilter)
			if qErr != nil {
				return qErr
			}

			galleryIDs = idsFromItems(result, func(item *models.Gallery) int {
				return item.ID
			})
			return nil
		})
		if err != nil {
			return "", err
		}
	}

	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	// Populate gallery from the input
	updatedGallery := models.NewGalleryPartial()

	updatedGallery.Code = translator.optionalString(input.Code, "code")
	updatedGallery.Details = translator.optionalString(input.Details, "details")
	updatedGallery.Photographer = translator.optionalString(input.Photographer, "photographer")
	updatedGallery.Rating = translator.optionalInt(input.Rating100, "rating100")
	updatedGallery.Organized = translator.optionalBool(input.Organized, "organized")
	updatedGallery.URLs = translator.optionalURLsBulk(input.Urls, input.URL)

	updatedGallery.Date, err = translator.optionalDate(input.Date, "date")
	if err != nil {
		return "", fmt.Errorf("converting date: %w", err)
	}
	updatedGallery.StudioID, err = translator.optionalIntFromString(input.StudioID, "studio_id")
	if err != nil {
		return "", fmt.Errorf("converting studio id: %w", err)
	}

	updatedGallery.PerformerIDs, err = translator.updateIdsBulk(input.PerformerIds, "performer_ids")
	if err != nil {
		return "", fmt.Errorf("converting performer ids: %w", err)
	}
	updatedGallery.TagIDs, err = translator.updateIdsBulk(input.TagIds, "tag_ids")
	if err != nil {
		return "", fmt.Errorf("converting tag ids: %w", err)
	}
	updatedGallery.SceneIDs, err = translator.updateIdsBulk(input.SceneIds, "scene_ids")
	if err != nil {
		return "", fmt.Errorf("converting scene ids: %w", err)
	}

	if input.CustomFields != nil {
		updatedGallery.CustomFields = handleUpdateCustomFields(*input.CustomFields)
	}

	operation := galleryBulkUpdateOperation{
		repository:     r.repository.Gallery,
		updatedGallery: updatedGallery,
	}

	if !useBackgroundJob {
		if err := r.withTxn(ctx, func(ctx context.Context) error {
			for _, galleryID := range galleryIDs {
				if err := operation.Update(ctx, galleryID); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return "", err
		}

		for _, galleryID := range galleryIDs {
			r.hookExecutor.ExecutePostHooks(ctx, galleryID, hook.GalleryUpdatePost, input, translator.getFields())
		}

		return "sync", nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Bulk Gallery Update", galleryIDs, operation, hook.GalleryUpdatePost, input, translator.getFields())

	return strconv.Itoa(jobID), nil
}
