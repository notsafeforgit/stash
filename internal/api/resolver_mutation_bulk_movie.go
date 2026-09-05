package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
)

// Fork bulk operations. Legacy resolvers delegate here with explicit IDs;
// only the additive job mutations may select items by filter.

type movieBulkUpdateOperation struct {
	repository   models.GroupReaderWriter
	updatedGroup models.GroupPartial
	hookExecutor hookExecutor
	input        interface{}
	inputFields  []string
}

func (o movieBulkUpdateOperation) Update(ctx context.Context, id int) error {
	_, err := o.repository.UpdatePartial(ctx, id, o.updatedGroup)

	// for backwards compatibility - run both movie and group hooks
	if err == nil && config.GetBulkUpdateHooks() && o.hookExecutor != nil {
		o.hookExecutor.ExecutePostHooks(ctx, id, hook.GroupUpdatePost, o.input, o.inputFields)
		o.hookExecutor.ExecutePostHooks(ctx, id, hook.MovieUpdatePost, o.input, o.inputFields)
	}

	return err
}

func (r *mutationResolver) BulkMovieUpdateJob(ctx context.Context, input BulkMovieUpdateInput) (string, error) {
	groupIDs, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return "", fmt.Errorf("converting ids: %w", err)
	}

	useBackgroundJob := (input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters) ||
		(len(input.Ids) == 0 && (input.FindFilter != nil || input.MovieFilter != nil))
	if useBackgroundJob && input.MovieFilter != nil {
		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		err = r.withReadTxn(ctx, func(ctx context.Context) error {
			result, _, qErr := r.repository.Group.Query(ctx, input.MovieFilter, findFilter)
			if qErr != nil {
				return qErr
			}
			var fetchedIds []int
			for _, item := range result {
				fetchedIds = append(fetchedIds, item.ID)
			}
			groupIDs = fetchedIds
			return nil
		})
		if err != nil {
			return "", err
		}
	}

	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	// Populate group from the input
	updatedGroup := models.NewGroupPartial()

	updatedGroup.Rating = translator.optionalInt(input.Rating100, "rating100")
	updatedGroup.Director = translator.optionalString(input.Director, "director")

	updatedGroup.StudioID, err = translator.optionalIntFromString(input.StudioID, "studio_id")
	if err != nil {
		return "", fmt.Errorf("converting studio id: %w", err)
	}

	updatedGroup.TagIDs, err = translator.updateIdsBulk(input.TagIds, "tag_ids")
	if err != nil {
		return "", fmt.Errorf("converting tag ids: %w", err)
	}

	updatedGroup.URLs = translator.optionalURLsBulk(input.Urls, nil)

	operation := movieBulkUpdateOperation{
		repository:   r.repository.Group,
		updatedGroup: updatedGroup,
		hookExecutor: r.hookExecutor,
		input:        input,
		inputFields:  translator.getFields(),
	}

	if !useBackgroundJob {
		syncOperation := operation
		syncOperation.hookExecutor = nil
		if err := r.withTxn(ctx, func(ctx context.Context) error {
			for _, groupID := range groupIDs {
				if err := syncOperation.Update(ctx, groupID); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return "", err
		}

		for _, groupID := range groupIDs {
			r.hookExecutor.ExecutePostHooks(ctx, groupID, hook.GroupUpdatePost, input, translator.getFields())
			r.hookExecutor.ExecutePostHooks(ctx, groupID, hook.MovieUpdatePost, input, translator.getFields())
		}

		return "sync", nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Bulk Movie Update", groupIDs, operation, hook.MovieUpdatePost, input, translator.getFields())

	return strconv.Itoa(jobID), nil
}
