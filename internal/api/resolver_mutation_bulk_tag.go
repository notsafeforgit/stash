package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
	"github.com/stashapp/stash/pkg/tag"
)

// Fork bulk operations. Legacy resolvers delegate here with explicit IDs;
// only the additive job mutations may select items by filter.

type tagBulkUpdateOperation struct {
	repository models.TagReaderWriter
	updatedTag models.TagPartial
}

func (o tagBulkUpdateOperation) Update(ctx context.Context, id int) error {
	if err := tag.ValidateUpdate(ctx, id, o.updatedTag, o.repository); err != nil {
		return err
	}

	_, err := o.repository.UpdatePartial(ctx, id, o.updatedTag)
	return err
}

func (r *mutationResolver) BulkTagUpdateJob(ctx context.Context, input BulkTagUpdateInput) (string, error) {
	tagIDs, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return "", fmt.Errorf("converting ids: %w", err)
	}

	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.TagFilterAst) {
			return "", fmt.Errorf("tag_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		err = r.withReadTxn(ctx, func(ctx context.Context) error {
			result, _, qErr := r.repository.Tag.QueryAST(ctx, input.TagFilterAst, findFilter)
			if qErr != nil {
				return qErr
			}

			tagIDs = idsFromItems(result, func(item *models.Tag) int {
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

	// Populate scene from the input
	updatedTag := models.NewTagPartial()

	updatedTag.Description = translator.optionalString(input.Description, "description")
	updatedTag.Favorite = translator.optionalBool(input.Favorite, "favorite")
	updatedTag.IgnoreAutoTag = translator.optionalBool(input.IgnoreAutoTag, "ignore_auto_tag")

	updatedTag.Aliases = translator.updateStringsBulk(input.Aliases, "aliases")

	updatedTag.ParentIDs, err = translator.updateIdsBulk(input.ParentIds, "parent_ids")
	if err != nil {
		return "", fmt.Errorf("converting parent tag ids: %w", err)
	}

	updatedTag.ChildIDs, err = translator.updateIdsBulk(input.ChildIds, "child_ids")
	if err != nil {
		return "", fmt.Errorf("converting child tag ids: %w", err)
	}

	operation := tagBulkUpdateOperation{
		repository: r.repository.Tag,
		updatedTag: updatedTag,
	}

	if !useBackgroundJob {
		if err := r.withTxn(ctx, func(ctx context.Context) error {
			for _, tagID := range tagIDs {
				if err := operation.Update(ctx, tagID); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return "", err
		}

		for _, tagID := range tagIDs {
			r.hookExecutor.ExecutePostHooks(ctx, tagID, hook.TagUpdatePost, input, translator.getFields())
		}

		return "sync", nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Bulk Tag Update", tagIDs, operation, hook.TagUpdatePost, input, translator.getFields())

	return strconv.Itoa(jobID), nil
}
