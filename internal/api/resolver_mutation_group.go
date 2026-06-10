package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/stashapp/stash/internal/entityimage"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/group"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
	"github.com/stashapp/stash/pkg/utils"
)

func (r *mutationResolver) groupFromGroupCreateInput(ctx context.Context, input GroupCreateInput) (*models.CreateGroupInput, error) {
	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	// Populate a new group from the input
	newGroupInput := &models.CreateGroupInput{
		Group: &models.Group{},
	}
	*newGroupInput.Group = models.NewGroup()
	newGroup := newGroupInput.Group

	newGroup.Name = strings.TrimSpace(input.Name)
	newGroup.Aliases = translator.string(input.Aliases)
	newGroup.Duration = input.Duration
	newGroup.Rating = input.Rating100
	newGroup.Director = translator.string(input.Director)
	newGroup.Synopsis = translator.string(input.Synopsis)

	var err error

	newGroup.Date, err = translator.datePtr(input.Date)
	if err != nil {
		return nil, fmt.Errorf("converting date: %w", err)
	}
	newGroup.StudioID, err = translator.intPtrFromString(input.StudioID)
	if err != nil {
		return nil, fmt.Errorf("converting studio id: %w", err)
	}

	newGroup.TagIDs, err = translator.relatedIds(input.TagIds)
	if err != nil {
		return nil, fmt.Errorf("converting tag ids: %w", err)
	}

	newGroup.ContainingGroups, err = translator.groupIDDescriptions(input.ContainingGroups)
	if err != nil {
		return nil, fmt.Errorf("converting containing group ids: %w", err)
	}

	newGroup.SubGroups, err = translator.groupIDDescriptions(input.SubGroups)
	if err != nil {
		return nil, fmt.Errorf("converting containing group ids: %w", err)
	}

	if input.Urls != nil {
		newGroup.URLs = models.NewRelatedStrings(stringslice.TrimSpace(input.Urls))
	}

	newGroupInput.CustomFields = convertMapJSONNumbers(input.CustomFields)

	if input.FrontImage != nil || input.FrontImageInput != nil {
		newGroupInput.FrontImageData, _, err = r.processEntityImageFields(ctx, input.FrontImage, input.FrontImageInput)
		if err != nil {
			return nil, fmt.Errorf("processing front image: %w", err)
		}
	}

	if input.BackImage != nil || input.BackImageInput != nil {
		newGroupInput.BackImageData, _, err = r.processEntityImageFields(ctx, input.BackImage, input.BackImageInput)
		if err != nil {
			return nil, fmt.Errorf("processing back image: %w", err)
		}
	}

	// HACK: if back image is being set, set the front image to the default.
	// This is because we can't have a null front image with a non-null back image.
	if len(newGroupInput.FrontImageData) == 0 && len(newGroupInput.BackImageData) != 0 {
		newGroupInput.FrontImageData = entityimage.DefaultGroupFrontImage
	}

	return newGroupInput, nil
}

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

func (r *mutationResolver) GroupCreate(ctx context.Context, input GroupCreateInput) (*models.Group, error) {
	createGroupInput, err := r.groupFromGroupCreateInput(ctx, input)
	if err != nil {
		return nil, err
	}

	// Start the transaction and save the group
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		if err = r.groupService.Create(ctx, createGroupInput); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	// for backwards compatibility - run both movie and group hooks
	r.hookExecutor.ExecutePostHooks(ctx, createGroupInput.Group.ID, hook.GroupCreatePost, input, nil)
	r.hookExecutor.ExecutePostHooks(ctx, createGroupInput.Group.ID, hook.MovieCreatePost, input, nil)
	return r.getGroup(ctx, createGroupInput.Group.ID)
}

func groupPartialFromGroupUpdateInput(translator changesetTranslator, input GroupUpdateInput) (ret models.GroupPartial, err error) {
	// Populate group from the input
	updatedGroup := models.NewGroupPartial()

	updatedGroup.Name = translator.optionalString(input.Name, "name")
	updatedGroup.Aliases = translator.optionalString(input.Aliases, "aliases")
	updatedGroup.Duration = translator.optionalInt(input.Duration, "duration")
	updatedGroup.Rating = translator.optionalInt(input.Rating100, "rating100")
	updatedGroup.Director = translator.optionalString(input.Director, "director")
	updatedGroup.Synopsis = translator.optionalString(input.Synopsis, "synopsis")

	updatedGroup.Date, err = translator.optionalDate(input.Date, "date")
	if err != nil {
		err = fmt.Errorf("converting date: %w", err)
		return
	}
	updatedGroup.StudioID, err = translator.optionalIntFromString(input.StudioID, "studio_id")
	if err != nil {
		err = fmt.Errorf("converting studio id: %w", err)
		return
	}

	updatedGroup.TagIDs, err = translator.updateIds(input.TagIds, "tag_ids")
	if err != nil {
		err = fmt.Errorf("converting tag ids: %w", err)
		return
	}

	updatedGroup.ContainingGroups, err = translator.updateGroupIDDescriptions(input.ContainingGroups, "containing_groups")
	if err != nil {
		err = fmt.Errorf("converting containing group ids: %w", err)
		return
	}

	updatedGroup.SubGroups, err = translator.updateGroupIDDescriptions(input.SubGroups, "sub_groups")
	if err != nil {
		err = fmt.Errorf("converting containing group ids: %w", err)
		return
	}

	updatedGroup.URLs = translator.updateStrings(input.Urls, "urls")
	if input.CustomFields != nil {
		updatedGroup.CustomFields = *input.CustomFields
		// convert json.Numbers to int/float
		updatedGroup.CustomFields.Full = convertMapJSONNumbers(updatedGroup.CustomFields.Full)
		updatedGroup.CustomFields.Partial = convertMapJSONNumbers(updatedGroup.CustomFields.Partial)
	}

	return updatedGroup, nil
}

func (r *mutationResolver) GroupUpdate(ctx context.Context, input GroupUpdateInput) (*models.Group, error) {
	groupID, err := strconv.Atoi(input.ID)
	if err != nil {
		return nil, fmt.Errorf("converting id: %w", err)
	}

	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	updatedGroup, err := groupPartialFromGroupUpdateInput(translator, input)
	if err != nil {
		return nil, err
	}

	var frontimageData []byte
	frontimageData, frontImageIncluded, err := r.processEntityImageFields(ctx, input.FrontImage, input.FrontImageInput)
	if err != nil {
		return nil, fmt.Errorf("processing front image: %w", err)
	}
	if !frontImageIncluded && (translator.hasField("front_image") || translator.hasField("front_image_input")) {
		frontImageIncluded = true
	}

	var backimageData []byte
	backimageData, backImageIncluded, err := r.processEntityImageFields(ctx, input.BackImage, input.BackImageInput)
	if err != nil {
		return nil, fmt.Errorf("processing back image: %w", err)
	}
	if !backImageIncluded && (translator.hasField("back_image") || translator.hasField("back_image_input")) {
		backImageIncluded = true
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		frontImage := group.ImageInput{
			Image: frontimageData,
			Set:   frontImageIncluded,
		}

		backImage := group.ImageInput{
			Image: backimageData,
			Set:   backImageIncluded,
		}

		_, err = r.groupService.UpdatePartial(ctx, groupID, updatedGroup, frontImage, backImage)
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	// for backwards compatibility - run both movie and group hooks
	r.hookExecutor.ExecutePostHooks(ctx, groupID, hook.GroupUpdatePost, input, translator.getFields())
	r.hookExecutor.ExecutePostHooks(ctx, groupID, hook.MovieUpdatePost, input, translator.getFields())
	return r.getGroup(ctx, groupID)
}

func groupPartialFromBulkGroupUpdateInput(translator changesetTranslator, input BulkGroupUpdateInput) (ret models.GroupPartial, err error) {
	updatedGroup := models.NewGroupPartial()

	updatedGroup.Date, err = translator.optionalDate(input.Date, "date")
	if err != nil {
		err = fmt.Errorf("converting date: %w", err)
		return
	}
	updatedGroup.Synopsis = translator.optionalString(input.Synopsis, "synopsis")
	updatedGroup.Rating = translator.optionalInt(input.Rating100, "rating100")
	updatedGroup.Director = translator.optionalString(input.Director, "director")

	updatedGroup.StudioID, err = translator.optionalIntFromString(input.StudioID, "studio_id")
	if err != nil {
		err = fmt.Errorf("converting studio id: %w", err)
		return
	}

	updatedGroup.TagIDs, err = translator.updateIdsBulk(input.TagIds, "tag_ids")
	if err != nil {
		err = fmt.Errorf("converting tag ids: %w", err)
		return
	}

	updatedGroup.ContainingGroups, err = translator.updateGroupIDDescriptionsBulk(input.ContainingGroups, "containing_groups")
	if err != nil {
		err = fmt.Errorf("converting containing group ids: %w", err)
		return
	}

	updatedGroup.SubGroups, err = translator.updateGroupIDDescriptionsBulk(input.SubGroups, "sub_groups")
	if err != nil {
		err = fmt.Errorf("converting containing group ids: %w", err)
		return
	}

	updatedGroup.URLs = translator.optionalURLsBulk(input.Urls, nil)

	if input.CustomFields != nil {
		updatedGroup.CustomFields = *input.CustomFields
		// convert json.Numbers to int/float
		updatedGroup.CustomFields.Full = convertMapJSONNumbers(updatedGroup.CustomFields.Full)
		updatedGroup.CustomFields.Partial = convertMapJSONNumbers(updatedGroup.CustomFields.Partial)
	}

	return updatedGroup, nil
}

func (r *mutationResolver) BulkGroupUpdate(ctx context.Context, input BulkGroupUpdateInput) ([]*models.Group, error) {
	groupIDs, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return nil, fmt.Errorf("converting ids: %w", err)
	}

	compatInput := input
	compatInput.ApplyToItemsMatchingFilters = nil
	compatInput.FindFilter = nil
	compatInput.GroupFilter = nil

	if _, err := r.BulkGroupUpdateJob(ctx, compatInput); err != nil {
		return nil, err
	}

	return refetchBulkUpdateResults(ctx, groupIDs, r.getGroup)
}

func (r *mutationResolver) BulkGroupUpdateJob(ctx context.Context, input BulkGroupUpdateInput) (string, error) {
	groupIDs, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return "", fmt.Errorf("converting ids: %w", err)
	}

	useBackgroundJob := (input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters) ||
		(len(input.Ids) == 0 && (input.FindFilter != nil || input.GroupFilter != nil))
	if useBackgroundJob {
		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		err = r.withReadTxn(ctx, func(ctx context.Context) error {
			result, _, qErr := r.repository.Group.Query(ctx, input.GroupFilter, findFilter)
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
	updatedGroup, err := groupPartialFromBulkGroupUpdateInput(translator, input)
	if err != nil {
		return "", err
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
			return "", err
		}

		for _, groupID := range groupIDs {
			r.hookExecutor.ExecutePostHooks(ctx, groupID, hook.GroupUpdatePost, input, translator.getFields())
			r.hookExecutor.ExecutePostHooks(ctx, groupID, hook.MovieUpdatePost, input, translator.getFields())
		}

		return "sync", nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Bulk Group Update", groupIDs, operation, hook.GroupUpdatePost, input, translator.getFields())

	return strconv.Itoa(jobID), nil
}

func (r *mutationResolver) GroupDestroy(ctx context.Context, input GroupDestroyInput) (bool, error) {
	id, err := strconv.Atoi(input.ID)
	if err != nil {
		return false, fmt.Errorf("converting id: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		return r.repository.Group.Destroy(ctx, id)
	}); err != nil {
		return false, err
	}

	// for backwards compatibility - run both movie and group hooks
	r.hookExecutor.ExecutePostHooks(ctx, id, hook.GroupDestroyPost, input, nil)
	r.hookExecutor.ExecutePostHooks(ctx, id, hook.MovieDestroyPost, input, nil)

	return true, nil
}

func (r *mutationResolver) GroupsDestroy(ctx context.Context, groupIDs []string) (bool, error) {
	ids, err := stringslice.StringSliceToIntSlice(groupIDs)
	if err != nil {
		return false, fmt.Errorf("converting ids: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Group
		for _, id := range ids {
			if err := qb.Destroy(ctx, id); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return false, err
	}

	for _, id := range ids {
		// for backwards compatibility - run both movie and group hooks
		r.hookExecutor.ExecutePostHooks(ctx, id, hook.GroupDestroyPost, groupIDs, nil)
		r.hookExecutor.ExecutePostHooks(ctx, id, hook.MovieDestroyPost, groupIDs, nil)
	}

	return true, nil
}

func (r *mutationResolver) AddGroupSubGroups(ctx context.Context, input GroupSubGroupAddInput) (bool, error) {
	groupID, err := strconv.Atoi(input.ContainingGroupID)
	if err != nil {
		return false, fmt.Errorf("converting group id: %w", err)
	}

	subGroups, err := groupsDescriptionsFromGroupInput(input.SubGroups)
	if err != nil {
		return false, fmt.Errorf("converting sub group ids: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		return r.groupService.AddSubGroups(ctx, groupID, subGroups, input.InsertIndex)
	}); err != nil {
		return false, err
	}

	return true, nil
}

func (r *mutationResolver) RemoveGroupSubGroups(ctx context.Context, input GroupSubGroupRemoveInput) (bool, error) {
	groupID, err := strconv.Atoi(input.ContainingGroupID)
	if err != nil {
		return false, fmt.Errorf("converting group id: %w", err)
	}

	subGroupIDs, err := stringslice.StringSliceToIntSlice(input.SubGroupIds)
	if err != nil {
		return false, fmt.Errorf("converting sub group ids: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		return r.groupService.RemoveSubGroups(ctx, groupID, subGroupIDs)
	}); err != nil {
		return false, err
	}

	return true, nil
}

func (r *mutationResolver) ReorderSubGroups(ctx context.Context, input ReorderSubGroupsInput) (bool, error) {
	groupID, err := strconv.Atoi(input.GroupID)
	if err != nil {
		return false, fmt.Errorf("converting group id: %w", err)
	}

	subGroupIDs, err := stringslice.StringSliceToIntSlice(input.SubGroupIds)
	if err != nil {
		return false, fmt.Errorf("converting sub group ids: %w", err)
	}

	insertPointID, err := strconv.Atoi(input.InsertAtID)
	if err != nil {
		return false, fmt.Errorf("converting insert at id: %w", err)
	}

	insertAfter := utils.IsTrue(input.InsertAfter)

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		return r.groupService.ReorderSubGroups(ctx, groupID, subGroupIDs, insertPointID, insertAfter)
	}); err != nil {
		return false, err
	}

	return true, nil
}
