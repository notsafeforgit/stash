package api

import (
	"context"

	"fmt"
	"github.com/stashapp/stash/pkg/bulkupdate"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
)

// Native bulk edits complete explicit IDs atomically or queue the selected
// filter results. Both paths return an explicit completion acknowledgment.

func (r *mutationResolver) BulkSceneUpdate(ctx context.Context, input BulkSceneUpdateInput) (*BulkUpdateResult, error) {
	var sceneIDs []int
	var err error
	var selection bulkupdate.Selector
	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if len(input.Ids) > 0 {
		sceneIDs, err = stringslice.StringSliceToIntSlice(input.Ids)
		if err != nil {
			return nil, fmt.Errorf("converting ids: %w", err)
		}
	}
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.SceneFilterAst) {
			return nil, fmt.Errorf("scene_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		selection = bulkupdate.SelectAll(r.repository.Scene.QueryAST, input.SceneFilterAst, input.FindFilter, func(item *models.Scene) int { return item.ID })
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
		return nil, fmt.Errorf("converting date: %w", err)
	}
	updatedScene.ProductionDate, err = translator.optionalDate(input.ProductionDate, "production_date")
	if err != nil {
		return nil, fmt.Errorf("converting production date: %w", err)
	}
	updatedScene.StudioID, err = translator.optionalIntFromString(input.StudioID, "studio_id")
	if err != nil {
		return nil, fmt.Errorf("converting studio id: %w", err)
	}

	updatedScene.URLs = translator.optionalURLsBulk(input.Urls, input.URL)

	updatedScene.PerformerIDs, err = translator.updateIdsBulk(input.PerformerIds, "performer_ids")
	if err != nil {
		return nil, fmt.Errorf("converting performer ids: %w", err)
	}
	updatedScene.TagIDs, err = translator.updateIdsBulk(input.TagIds, "tag_ids")
	if err != nil {
		return nil, fmt.Errorf("converting tag ids: %w", err)
	}
	updatedScene.GalleryIDs, err = translator.updateIdsBulk(input.GalleryIds, "gallery_ids")
	if err != nil {
		return nil, fmt.Errorf("converting gallery ids: %w", err)
	}

	if translator.hasField("group_ids") {
		updatedScene.GroupIDs, err = translator.updateGroupIDsBulk(input.GroupIds, "group_ids")
		if err != nil {
			return nil, fmt.Errorf("converting group ids: %w", err)
		}
	}

	var customFields *models.CustomFieldsInput
	if input.CustomFields != nil {
		cf := handleUpdateCustomFields(*input.CustomFields)
		customFields = &cf
	}

	operation := bulkupdate.Scene{
		Repository:   r.repository.Scene,
		Changes:      updatedScene,
		CustomFields: customFields,
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
			return nil, err
		}

		for _, sceneID := range sceneIDs {
			r.hookExecutor.ExecutePostHooks(ctx, sceneID, hook.SceneUpdatePost, input, translator.getFields())
		}

		return completedBulkUpdate(sceneIDs), nil
	}

	jobID, selectedCount, err := r.enqueueBulkUpdate(ctx, "Bulk Scene Update", selection, operation, hook.SceneUpdatePost, input, translator.getFields())
	if err != nil {
		return nil, err
	}

	return queuedBulkUpdate(jobID, selectedCount), nil
}

func (r *mutationResolver) ScenesSetDateFromFileMTime(ctx context.Context, input ScenesSetDateFromFileMTimeInput) (string, error) {
	var sceneIDs []int
	var err error
	var selection bulkupdate.Selector
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

		selection = bulkupdate.SelectAll(r.repository.Scene.QueryAST, input.SceneFilterAst, input.FindFilter, func(item *models.Scene) int { return item.ID })
	}

	operation := bulkupdate.SceneDateFromMTime{
		Repository: r.repository.Scene,
		Files:      r.repository.File,
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

	jobID, _, err := r.enqueueBulkUpdate(ctx, "Set Scene Dates From File MTime", selection, operation, hook.SceneUpdatePost, input, []string{"date"})
	return jobID, err
}

func (r *mutationResolver) BulkSceneMarkerUpdate(ctx context.Context, input BulkSceneMarkerUpdateInput) (*BulkUpdateResult, error) {
	ids, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return nil, fmt.Errorf("converting ids: %w", err)
	}

	var selection bulkupdate.Selector
	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.SceneMarkerFilterAst) {
			return nil, fmt.Errorf("scene_marker_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		selection = bulkupdate.SelectAll(r.repository.SceneMarker.QueryAST, input.SceneMarkerFilterAst, input.FindFilter, func(item *models.SceneMarker) int { return item.ID })
	}

	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	// Populate scene marker from the input
	partial := models.NewSceneMarkerPartial()

	partial.Title = translator.optionalString(input.Title, "title")

	partial.PrimaryTagID, err = translator.optionalIntFromString(input.PrimaryTagID, "primary_tag_id")
	if err != nil {
		return nil, fmt.Errorf("converting primary tag id: %w", err)
	}

	partial.TagIDs, err = translator.updateIdsBulk(input.TagIds, "tag_ids")
	if err != nil {
		return nil, fmt.Errorf("converting tag ids: %w", err)
	}

	operation := bulkupdate.SceneMarker{
		Repository: r.repository.SceneMarker,
		Changes:    partial,
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
			return nil, err
		}

		for _, id := range ids {
			r.hookExecutor.ExecutePostHooks(ctx, id, hook.SceneMarkerUpdatePost, input, translator.getFields())
		}

		return completedBulkUpdate(ids), nil
	}

	jobDescription := "Bulk Scene Marker Update"
	jobID, selectedCount, err := r.enqueueBulkUpdate(ctx, jobDescription, selection, operation, hook.SceneMarkerUpdatePost, input, translator.getFields())
	if err != nil {
		return nil, err
	}

	return queuedBulkUpdate(jobID, selectedCount), nil
}
