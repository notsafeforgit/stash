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

func (r *mutationResolver) BulkStudioUpdate(ctx context.Context, input BulkStudioUpdateInput) (*BulkUpdateResult, error) {
	ids, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return nil, fmt.Errorf("converting ids: %w", err)
	}

	var selection bulkupdate.Selector
	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.StudioFilterAst) {
			return nil, fmt.Errorf("studio_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		selection = bulkupdate.SelectAll(r.repository.Studio.QueryAST, input.StudioFilterAst, input.FindFilter, func(item *models.Studio) int { return item.ID })
	}

	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	// Populate performer from the input
	partial := models.NewStudioPartial()

	partial.ParentID, err = translator.optionalIntFromString(input.ParentID, "parent_id")
	if err != nil {
		return nil, fmt.Errorf("converting parent id: %w", err)
	}

	if translator.hasField("urls") {
		// ensure url/twitter/instagram are not included in the input
		if err := validateNoLegacyURLs(translator); err != nil {
			return nil, err
		}

		partial.URLs = translator.updateStringsBulk(input.Urls, "urls")
	} else if translator.hasField("url") {
		// handle legacy url field
		legacyURLs := []string{}
		if input.URL != nil {
			legacyURLs = append(legacyURLs, *input.URL)
		}

		partial.URLs = &models.UpdateStrings{
			Mode:   models.RelationshipUpdateModeSet,
			Values: legacyURLs,
		}
	}

	partial.Favorite = translator.optionalBool(input.Favorite, "favorite")
	partial.Rating = translator.optionalInt(input.Rating100, "rating100")
	partial.Details = translator.optionalString(input.Details, "details")
	partial.IgnoreAutoTag = translator.optionalBool(input.IgnoreAutoTag, "ignore_auto_tag")
	partial.Organized = translator.optionalBool(input.Organized, "organized")
	if input.CustomFields != nil {
		partial.CustomFields = handleUpdateCustomFields(*input.CustomFields)
	}

	partial.TagIDs, err = translator.updateIdsBulk(input.TagIds, "tag_ids")
	if err != nil {
		return nil, fmt.Errorf("converting tag ids: %w", err)
	}

	operation := bulkupdate.Studio{
		Repository: r.repository.Studio,
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
			r.hookExecutor.ExecutePostHooks(ctx, id, hook.StudioUpdatePost, input, translator.getFields())
		}

		return completedBulkUpdate(ids), nil
	}

	jobID, selectedCount, err := r.enqueueBulkUpdate(ctx, "Bulk Studio Update", selection, operation, hook.StudioUpdatePost, input, translator.getFields())
	if err != nil {
		return nil, err
	}

	return queuedBulkUpdate(jobID, selectedCount), nil
}
