package api

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/group"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
)

// Native bulk edits complete explicit IDs atomically or queue the selected
// filter results. Both paths return an explicit completion acknowledgment.

type groupBulkUpdateOperation struct {
	groupService   manager.GroupService
	updatedGroup   models.GroupPartial
	hookExecutor   hookExecutor
	input          interface{}
	inputFields    []string
	runCompatHooks bool
}

func (o groupBulkUpdateOperation) Update(ctx context.Context, id int) error {
	_, err := o.groupService.UpdatePartial(ctx, id, o.updatedGroup, group.ImageInput{}, group.ImageInput{})

	// for backwards compatibility - run both movie and group hooks
	// BulkUpdate will run the GroupUpdatePost hook, we manually run MovieUpdatePost
	if err == nil && o.runCompatHooks && o.hookExecutor != nil {
		o.hookExecutor.ExecutePostHooks(ctx, id, hook.MovieUpdatePost, o.input, o.inputFields)
	}

	return err
}

func (r *mutationResolver) BulkGroupUpdate(ctx context.Context, input BulkGroupUpdateInput) (*BulkUpdateResult, error) {
	groupIDs, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return nil, fmt.Errorf("converting ids: %w", err)
	}

	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.GroupFilterAst) {
			return nil, fmt.Errorf("group_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		err = r.withReadTxn(ctx, func(ctx context.Context) error {
			result, _, qErr := r.repository.Group.QueryAST(ctx, input.GroupFilterAst, findFilter)
			if qErr != nil {
				return qErr
			}

			groupIDs = idsFromItems(result, func(item *models.Group) int {
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

	// Populate group from the input
	updatedGroup, err := groupPartialFromBulkGroupUpdateInput(translator, input)
	if err != nil {
		return nil, err
	}

	operation := groupBulkUpdateOperation{
		groupService:   r.groupService,
		updatedGroup:   updatedGroup,
		hookExecutor:   r.hookExecutor,
		input:          input,
		inputFields:    translator.getFields(),
		runCompatHooks: true,
	}
	if useBackgroundJob {
		operation.runCompatHooks = config.GetBulkUpdateHooks()
	}

	if !useBackgroundJob {
		operation.runCompatHooks = false
		if err := r.withTxn(ctx, func(ctx context.Context) error {
			for _, groupID := range groupIDs {
				if err := operation.Update(ctx, groupID); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}

		for _, groupID := range groupIDs {
			r.hookExecutor.ExecutePostHooks(ctx, groupID, hook.GroupUpdatePost, input, translator.getFields())
			r.hookExecutor.ExecutePostHooks(ctx, groupID, hook.MovieUpdatePost, input, translator.getFields())
		}

		return completedBulkUpdate(groupIDs), nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Bulk Group Update", groupIDs, operation, hook.GroupUpdatePost, input, translator.getFields())

	return queuedBulkUpdate(jobID, len(groupIDs)), nil
}
