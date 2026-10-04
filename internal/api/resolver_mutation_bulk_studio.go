package api

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
	"github.com/stashapp/stash/pkg/studio"
)

// Native bulk edits complete explicit IDs atomically or queue the selected
// filter results. Both paths return an explicit completion acknowledgment.

type studioBulkUpdateOperation struct {
	repository models.StudioReaderWriter
	partial    models.StudioPartial
}

func (o studioBulkUpdateOperation) Update(ctx context.Context, id int) error {
	local := o.partial
	local.ID = id
	if err := studio.ValidateModify(ctx, local, o.repository); err != nil {
		return err
	}

	_, err := o.repository.UpdatePartial(ctx, local)
	return err
}

func (r *mutationResolver) BulkStudioUpdate(ctx context.Context, input BulkStudioUpdateInput) (*BulkUpdateResult, error) {
	ids, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return nil, fmt.Errorf("converting ids: %w", err)
	}

	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.StudioFilterAst) {
			return nil, fmt.Errorf("studio_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		err = r.withReadTxn(ctx, func(ctx context.Context) error {
			result, _, qErr := r.repository.Studio.QueryAST(ctx, input.StudioFilterAst, findFilter)
			if qErr != nil {
				return qErr
			}

			ids = idsFromItems(result, func(item *models.Studio) int {
				return item.ID
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
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

	operation := studioBulkUpdateOperation{
		repository: r.repository.Studio,
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
			return nil, err
		}

		for _, id := range ids {
			r.hookExecutor.ExecutePostHooks(ctx, id, hook.StudioUpdatePost, input, translator.getFields())
		}

		return completedBulkUpdate(ids), nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Bulk Studio Update", ids, operation, hook.StudioUpdatePost, input, translator.getFields())

	return queuedBulkUpdate(jobID, len(ids)), nil
}
