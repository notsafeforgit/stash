package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
)

// Fork bulk operations. Legacy resolvers delegate here with explicit IDs;
// only the additive job mutations may select items by filter.

type imageBulkUpdateOperation struct {
	repository     models.ImageReaderWriter
	galleryService manager.GalleryService
	updatedImage   models.ImagePartial
}

func (o imageBulkUpdateOperation) Update(ctx context.Context, id int) error {
	i, err := o.repository.Find(ctx, id)
	if err != nil {
		return err
	}
	if i == nil {
		return fmt.Errorf("image with id %d not found", id)
	}

	if o.updatedImage.GalleryIDs != nil {
		if err := i.LoadGalleryIDs(ctx, o.repository); err != nil {
			return err
		}

		if err := o.galleryService.ValidateImageGalleryChange(ctx, i, *o.updatedImage.GalleryIDs); err != nil {
			return err
		}
	}

	if _, err := o.repository.UpdatePartial(ctx, id, o.updatedImage); err != nil {
		return err
	}

	if o.updatedImage.GalleryIDs != nil {
		thisUpdatedGalleryIDs := o.updatedImage.GalleryIDs.ImpactedIDs(i.GalleryIDs.List())
		for _, galleryID := range thisUpdatedGalleryIDs {
			if err := o.galleryService.Updated(ctx, galleryID); err != nil {
				return fmt.Errorf("updating gallery %d: %w", galleryID, err)
			}
		}
	}

	return nil
}

func (r *mutationResolver) BulkImageUpdateJob(ctx context.Context, input BulkImageUpdateInput) (string, error) {
	imageIDs, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return "", fmt.Errorf("converting ids: %w", err)
	}

	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.ImageFilterAst) {
			return "", fmt.Errorf("image_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		err = r.withReadTxn(ctx, func(ctx context.Context) error {
			result, _, qErr := r.repository.Image.QueryAST(ctx, input.ImageFilterAst, findFilter)
			if qErr != nil {
				return qErr
			}

			imageIDs = idsFromItems(result, func(item *models.Image) int {
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

	// Populate image from the input
	updatedImage := models.NewImagePartial()

	updatedImage.Title = translator.optionalString(input.Title, "title")
	updatedImage.Code = translator.optionalString(input.Code, "code")
	updatedImage.Details = translator.optionalString(input.Details, "details")
	updatedImage.Photographer = translator.optionalString(input.Photographer, "photographer")
	updatedImage.Rating = translator.optionalInt(input.Rating100, "rating100")
	updatedImage.Organized = translator.optionalBool(input.Organized, "organized")

	updatedImage.Date, err = translator.optionalDate(input.Date, "date")
	if err != nil {
		return "", fmt.Errorf("converting date: %w", err)
	}
	updatedImage.StudioID, err = translator.optionalIntFromString(input.StudioID, "studio_id")
	if err != nil {
		return "", fmt.Errorf("converting studio id: %w", err)
	}

	updatedImage.URLs = translator.optionalURLsBulk(input.Urls, input.URL)

	updatedImage.GalleryIDs, err = translator.updateIdsBulk(input.GalleryIds, "gallery_ids")
	if err != nil {
		return "", fmt.Errorf("converting gallery ids: %w", err)
	}
	updatedImage.PerformerIDs, err = translator.updateIdsBulk(input.PerformerIds, "performer_ids")
	if err != nil {
		return "", fmt.Errorf("converting performer ids: %w", err)
	}
	updatedImage.TagIDs, err = translator.updateIdsBulk(input.TagIds, "tag_ids")
	if err != nil {
		return "", fmt.Errorf("converting tag ids: %w", err)
	}

	if input.CustomFields != nil {
		updatedImage.CustomFields = *input.CustomFields
		// convert json.Numbers to int/float
		updatedImage.CustomFields.Full = convertMapJSONNumbers(updatedImage.CustomFields.Full)
		updatedImage.CustomFields.Partial = convertMapJSONNumbers(updatedImage.CustomFields.Partial)
	}

	operation := imageBulkUpdateOperation{
		repository:     r.repository.Image,
		galleryService: r.galleryService,
		updatedImage:   updatedImage,
	}

	if !useBackgroundJob {
		if err := r.withTxn(ctx, func(ctx context.Context) error {
			for _, imageID := range imageIDs {
				if err := operation.Update(ctx, imageID); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return "", err
		}

		for _, imageID := range imageIDs {
			r.hookExecutor.ExecutePostHooks(ctx, imageID, hook.ImageUpdatePost, input, translator.getFields())
		}

		return "sync", nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Bulk Image Update", imageIDs, operation, hook.ImageUpdatePost, input, translator.getFields())

	return strconv.Itoa(jobID), nil
}

type imageSetDateFromMTimeOperation struct {
	repository models.ImageReaderWriter
	fileGetter models.FileGetter
}

func (o imageSetDateFromMTimeOperation) Update(ctx context.Context, id int) error {
	i, err := o.repository.Find(ctx, id)
	if err != nil {
		return err
	}
	if i == nil {
		return fmt.Errorf("image with id %d not found", id)
	}

	if err := i.LoadPrimaryFile(ctx, o.fileGetter); err != nil {
		return fmt.Errorf("loading primary file: %w", err)
	}
	primary := i.Files.Primary()
	if primary == nil {
		return nil
	}

	mtime := primary.Base().ModTime
	partial := models.NewImagePartial()
	partial.Date = models.NewOptionalDate(models.Date{Time: mtime, Precision: models.DatePrecisionDay})

	if _, err := o.repository.UpdatePartial(ctx, id, partial); err != nil {
		return err
	}
	return nil
}

func (r *mutationResolver) ImagesSetDateFromFileMTime(ctx context.Context, input ImagesSetDateFromFileMTimeInput) (string, error) {
	var imageIDs []int
	var err error
	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if len(input.Ids) > 0 {
		imageIDs, err = stringslice.StringSliceToIntSlice(input.Ids)
		if err != nil {
			return "", fmt.Errorf("converting ids: %w", err)
		}
	}
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.ImageFilterAst) {
			return "", fmt.Errorf("image_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		if err := r.withReadTxn(ctx, func(ctx context.Context) error {
			images, _, err := r.repository.Image.QueryAST(ctx, input.ImageFilterAst, findFilter)
			if err != nil {
				return err
			}

			imageIDs = idsFromItems(images, func(image *models.Image) int {
				return image.ID
			})
			return nil
		}); err != nil {
			return "", fmt.Errorf("querying ids: %w", err)
		}
	}

	operation := imageSetDateFromMTimeOperation{
		repository: r.repository.Image,
		fileGetter: r.repository.File,
	}

	if !useBackgroundJob {
		if err := r.withTxn(ctx, func(ctx context.Context) error {
			for _, id := range imageIDs {
				if err := operation.Update(ctx, id); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return "", err
		}

		for _, id := range imageIDs {
			r.hookExecutor.ExecutePostHooks(ctx, id, hook.ImageUpdatePost, input, []string{"date"})
		}

		return "sync", nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Set Image Dates From File MTime", imageIDs, operation, hook.ImageUpdatePost, input, []string{"date"})
	return strconv.Itoa(jobID), nil
}
