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

type sceneBulkUpdateOperation struct {
	repository   models.SceneReaderWriter
	updatedScene models.ScenePartial
	customFields *models.CustomFieldsInput
}

func (o sceneBulkUpdateOperation) Update(ctx context.Context, id int) error {
	scene, err := o.repository.UpdatePartial(ctx, id, o.updatedScene)
	if err != nil {
		return err
	}

	if o.customFields != nil {
		if err := o.repository.SetCustomFields(ctx, scene.ID, *o.customFields); err != nil {
			return err
		}
	}

	return nil
}

type sceneMarkerBulkUpdateOperation struct {
	repository models.SceneMarkerReaderWriter
	partial    models.SceneMarkerPartial
}

func (o sceneMarkerBulkUpdateOperation) Update(ctx context.Context, id int) error {
	partial := o.partial
	if err := adjustMarkerPartialForTagExclusion(ctx, o.repository, id, &partial); err != nil {
		return err
	}

	_, err := o.repository.UpdatePartial(ctx, id, partial)
	return err
}

func (r *mutationResolver) BulkSceneUpdateJob(ctx context.Context, input BulkSceneUpdateInput) (string, error) {
	var sceneIDs []int
	var err error
	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if len(input.Ids) > 0 {
		sceneIDs, err = stringslice.StringSliceToIntSlice(input.Ids)
		if err != nil {
			return "", fmt.Errorf("converting ids: %w", err)
		}
	}
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.SceneFilterAst) {
			return "", fmt.Errorf("scene_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		if err := r.withReadTxn(ctx, func(ctx context.Context) error {
			scenes, _, err := r.repository.Scene.QueryAST(ctx, input.SceneFilterAst, findFilter)
			if err != nil {
				return err
			}

			sceneIDs = idsFromItems(scenes, func(scene *models.Scene) int {
				return scene.ID
			})
			return nil
		}); err != nil {
			return "", fmt.Errorf("querying ids: %w", err)
		}
	}

	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	// Populate scene from the input
	updatedScene := models.NewScenePartial()

	updatedScene.Title = translator.optionalString(input.Title, "title")
	updatedScene.Code = translator.optionalString(input.Code, "code")
	updatedScene.Details = translator.optionalString(input.Details, "details")
	updatedScene.Director = translator.optionalString(input.Director, "director")
	updatedScene.Rating = translator.optionalInt(input.Rating100, "rating100")
	updatedScene.Organized = translator.optionalBool(input.Organized, "organized")

	updatedScene.Date, err = translator.optionalDate(input.Date, "date")
	if err != nil {
		return "", fmt.Errorf("converting date: %w", err)
	}
	updatedScene.StudioID, err = translator.optionalIntFromString(input.StudioID, "studio_id")
	if err != nil {
		return "", fmt.Errorf("converting studio id: %w", err)
	}

	updatedScene.URLs = translator.optionalURLsBulk(input.Urls, input.URL)

	updatedScene.PerformerIDs, err = translator.updateIdsBulk(input.PerformerIds, "performer_ids")
	if err != nil {
		return "", fmt.Errorf("converting performer ids: %w", err)
	}
	updatedScene.TagIDs, err = translator.updateIdsBulk(input.TagIds, "tag_ids")
	if err != nil {
		return "", fmt.Errorf("converting tag ids: %w", err)
	}
	updatedScene.GalleryIDs, err = translator.updateIdsBulk(input.GalleryIds, "gallery_ids")
	if err != nil {
		return "", fmt.Errorf("converting gallery ids: %w", err)
	}

	if translator.hasField("group_ids") {
		updatedScene.GroupIDs, err = translator.updateGroupIDsBulk(input.GroupIds, "group_ids")
		if err != nil {
			return "", fmt.Errorf("converting group ids: %w", err)
		}
	} else if translator.hasField("movie_ids") {
		updatedScene.GroupIDs, err = translator.updateGroupIDsBulk(input.MovieIds, "movie_ids")
		if err != nil {
			return "", fmt.Errorf("converting movie ids: %w", err)
		}
	}

	var customFields *models.CustomFieldsInput
	if input.CustomFields != nil {
		cf := handleUpdateCustomFields(*input.CustomFields)
		customFields = &cf
	}

	operation := sceneBulkUpdateOperation{
		repository:   r.repository.Scene,
		updatedScene: updatedScene,
		customFields: customFields,
	}

	if !useBackgroundJob {
		if err := r.withTxn(ctx, func(ctx context.Context) error {
			for _, sceneID := range sceneIDs {
				if err := operation.Update(ctx, sceneID); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return "", err
		}

		for _, sceneID := range sceneIDs {
			r.hookExecutor.ExecutePostHooks(ctx, sceneID, hook.SceneUpdatePost, input, translator.getFields())
		}

		return "sync", nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Bulk Scene Update", sceneIDs, operation, hook.SceneUpdatePost, input, translator.getFields())

	return strconv.Itoa(jobID), nil
}

type sceneSetDateFromMTimeOperation struct {
	repository models.SceneReaderWriter
	fileGetter models.FileGetter
}

func (o sceneSetDateFromMTimeOperation) Update(ctx context.Context, id int) error {
	s, err := o.repository.Find(ctx, id)
	if err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("scene with id %d not found", id)
	}

	if err := s.LoadPrimaryFile(ctx, o.fileGetter); err != nil {
		return fmt.Errorf("loading primary file: %w", err)
	}
	primary := s.Files.Primary()
	if primary == nil {
		// no primary file — nothing to derive from; skip silently so
		// bulk runs don't abort on isolated cases.
		return nil
	}

	mtime := primary.Base().ModTime
	partial := models.NewScenePartial()
	partial.Date = models.NewOptionalDate(models.Date{Time: mtime, Precision: models.DatePrecisionDay})

	if _, err := o.repository.UpdatePartial(ctx, id, partial); err != nil {
		return err
	}
	return nil
}

func (r *mutationResolver) ScenesSetDateFromFileMTime(ctx context.Context, input ScenesSetDateFromFileMTimeInput) (string, error) {
	var sceneIDs []int
	var err error
	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if len(input.Ids) > 0 {
		sceneIDs, err = stringslice.StringSliceToIntSlice(input.Ids)
		if err != nil {
			return "", fmt.Errorf("converting ids: %w", err)
		}
	}
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.SceneFilterAst) {
			return "", fmt.Errorf("scene_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		if err := r.withReadTxn(ctx, func(ctx context.Context) error {
			scenes, _, err := r.repository.Scene.QueryAST(ctx, input.SceneFilterAst, findFilter)
			if err != nil {
				return err
			}

			sceneIDs = idsFromItems(scenes, func(scene *models.Scene) int {
				return scene.ID
			})
			return nil
		}); err != nil {
			return "", fmt.Errorf("querying ids: %w", err)
		}
	}

	operation := sceneSetDateFromMTimeOperation{
		repository: r.repository.Scene,
		fileGetter: r.repository.File,
	}

	if !useBackgroundJob {
		if err := r.withTxn(ctx, func(ctx context.Context) error {
			for _, id := range sceneIDs {
				if err := operation.Update(ctx, id); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return "", err
		}

		// Fire the same hook as a regular scene update so plugins/listeners
		// see the date change.
		for _, id := range sceneIDs {
			r.hookExecutor.ExecutePostHooks(ctx, id, hook.SceneUpdatePost, input, []string{"date"})
		}

		return "sync", nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Set Scene Dates From File MTime", sceneIDs, operation, hook.SceneUpdatePost, input, []string{"date"})
	return strconv.Itoa(jobID), nil
}

func (r *mutationResolver) BulkSceneMarkerUpdateJob(ctx context.Context, input BulkSceneMarkerUpdateInput) (string, error) {
	ids, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return "", fmt.Errorf("converting ids: %w", err)
	}

	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.SceneMarkerFilterAst) {
			return "", fmt.Errorf("scene_marker_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		err = r.withReadTxn(ctx, func(ctx context.Context) error {
			result, _, qErr := r.repository.SceneMarker.QueryAST(ctx, input.SceneMarkerFilterAst, findFilter)
			if qErr != nil {
				return qErr
			}

			ids = idsFromItems(result, func(item *models.SceneMarker) int {
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

	// Populate scene marker from the input
	partial := models.NewSceneMarkerPartial()

	partial.Title = translator.optionalString(input.Title, "title")

	partial.PrimaryTagID, err = translator.optionalIntFromString(input.PrimaryTagID, "primary_tag_id")
	if err != nil {
		return "", fmt.Errorf("converting primary tag id: %w", err)
	}

	partial.TagIDs, err = translator.updateIdsBulk(input.TagIds, "tag_ids")
	if err != nil {
		return "", fmt.Errorf("converting tag ids: %w", err)
	}

	operation := sceneMarkerBulkUpdateOperation{
		repository: r.repository.SceneMarker,
		partial:    partial,
	}

	if !useBackgroundJob {
		if err := r.withTxn(ctx, func(ctx context.Context) error {
			for _, id := range ids {
				if err := operation.Update(ctx, id); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return "", err
		}

		for _, id := range ids {
			r.hookExecutor.ExecutePostHooks(ctx, id, hook.SceneMarkerUpdatePost, input, translator.getFields())
		}

		return "sync", nil
	}

	jobDescription := fmt.Sprintf("Bulk Scene Marker Update (%d items)", len(ids))
	jobID := r.enqueueBulkUpdate(ctx, jobDescription, ids, operation, hook.SceneMarkerUpdatePost, input, translator.getFields())

	return strconv.Itoa(jobID), nil
}
