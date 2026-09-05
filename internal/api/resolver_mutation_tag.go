package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
	"github.com/stashapp/stash/pkg/tag"
)

func (r *mutationResolver) getTag(ctx context.Context, id int) (ret *models.Tag, err error) {
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.Tag.Find(ctx, id)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *mutationResolver) TagCreate(ctx context.Context, input TagCreateInput) (*models.Tag, error) {
	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	// Populate a new tag from the input
	newTag := models.CreateTagInput{
		Tag: &models.Tag{},
	}
	*newTag.Tag = models.NewTag()

	newTag.Name = strings.TrimSpace(input.Name)
	newTag.SortName = translator.string(input.SortName)
	newTag.Aliases = models.NewRelatedStrings(stringslice.UniqueExcludeFold(stringslice.TrimSpace(input.Aliases), newTag.Name))
	newTag.Favorite = translator.bool(input.Favorite)
	newTag.Description = translator.string(input.Description)
	newTag.IgnoreAutoTag = translator.bool(input.IgnoreAutoTag)

	var stashIDInputs models.StashIDInputs
	for _, sid := range input.StashIds {
		if sid != nil {
			stashIDInputs = append(stashIDInputs, *sid)
		}
	}
	newTag.StashIDs = models.NewRelatedStashIDs(stashIDInputs.ToStashIDs())

	var err error

	newTag.ParentIDs, err = translator.relatedIds(input.ParentIds)
	if err != nil {
		return nil, fmt.Errorf("converting parent tag ids: %w", err)
	}

	newTag.ChildIDs, err = translator.relatedIds(input.ChildIds)
	if err != nil {
		return nil, fmt.Errorf("converting child tag ids: %w", err)
	}

	newTag.CustomFields = convertMapJSONNumbers(input.CustomFields)

	var imageData []byte
	if input.Image != nil || input.ImageInput != nil {
		imageData, _, err = r.processEntityImageFields(ctx, input.Image, input.ImageInput)
		if err != nil {
			return nil, err
		}
	}

	// Start the transaction and save the tag
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Tag

		if err := tag.ValidateCreate(ctx, *newTag.Tag, qb); err != nil {
			return err
		}

		err = qb.Create(ctx, &newTag)
		if err != nil {
			return err
		}

		// update image table
		if len(imageData) > 0 {
			if err := qb.UpdateImage(ctx, newTag.ID, imageData); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	r.hookExecutor.ExecutePostHooks(ctx, newTag.ID, hook.TagCreatePost, input, nil)
	return r.getTag(ctx, newTag.ID)
}

func tagPartialFromInput(input TagUpdateInput, translator changesetTranslator) (*models.TagPartial, error) {
	updatedTag := models.NewTagPartial()

	updatedTag.Name = translator.optionalString(input.Name, "name")
	updatedTag.SortName = translator.optionalString(input.SortName, "sort_name")
	updatedTag.Favorite = translator.optionalBool(input.Favorite, "favorite")
	updatedTag.IgnoreAutoTag = translator.optionalBool(input.IgnoreAutoTag, "ignore_auto_tag")
	updatedTag.Description = translator.optionalString(input.Description, "description")

	updatedTag.Aliases = translator.updateStrings(input.Aliases, "aliases")

	var updateStashIDInputs models.StashIDInputs
	for _, sid := range input.StashIds {
		if sid != nil {
			updateStashIDInputs = append(updateStashIDInputs, *sid)
		}
	}
	updatedTag.StashIDs = translator.updateStashIDs(updateStashIDInputs, "stash_ids")

	var err error
	updatedTag.ParentIDs, err = translator.updateIds(input.ParentIds, "parent_ids")
	if err != nil {
		return nil, fmt.Errorf("converting parent tag ids: %w", err)
	}

	updatedTag.ChildIDs, err = translator.updateIds(input.ChildIds, "child_ids")
	if err != nil {
		return nil, fmt.Errorf("converting child tag ids: %w", err)
	}

	if input.CustomFields != nil {
		updatedTag.CustomFields = *input.CustomFields
		// convert json.Numbers to int/float
		updatedTag.CustomFields.Full = convertMapJSONNumbers(updatedTag.CustomFields.Full)
		updatedTag.CustomFields.Partial = convertMapJSONNumbers(updatedTag.CustomFields.Partial)
	}

	return &updatedTag, nil
}

func (r *mutationResolver) TagUpdate(ctx context.Context, input TagUpdateInput) (*models.Tag, error) {
	tagID, err := strconv.Atoi(input.ID)
	if err != nil {
		return nil, fmt.Errorf("converting id: %w", err)
	}

	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	// Populate tag from the input
	updatedTag, err := tagPartialFromInput(input, translator)
	if err != nil {
		return nil, err
	}

	var imageData []byte
	imageData, imageIncluded, err := r.processEntityImageFields(ctx, input.Image, input.ImageInput)
	if err != nil {
		return nil, err
	}
	if !imageIncluded && (translator.hasField("image") || translator.hasField("image_input")) {
		imageIncluded = true
	}

	// Start the transaction and save the tag
	var t *models.Tag
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Tag

		if updatedTag.Aliases != nil {
			t, err := qb.Find(ctx, tagID)
			if err != nil {
				return err
			}
			if t != nil {
				if err := t.LoadAliases(ctx, qb); err != nil {
					return err
				}

				newAliases := updatedTag.Aliases.Apply(t.Aliases.List())
				name := t.Name
				if updatedTag.Name.Set {
					name = updatedTag.Name.Value
				}

				sanitized := stringslice.UniqueExcludeFold(newAliases, name)
				updatedTag.Aliases.Values = sanitized
				updatedTag.Aliases.Mode = models.RelationshipUpdateModeSet
			}
		}

		if err := tag.ValidateUpdate(ctx, tagID, *updatedTag, qb); err != nil {
			return err
		}

		t, err = qb.UpdatePartial(ctx, tagID, *updatedTag)
		if err != nil {
			return err
		}

		// update image table
		if imageIncluded {
			if err := qb.UpdateImage(ctx, tagID, imageData); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	r.hookExecutor.ExecutePostHooks(ctx, t.ID, hook.TagUpdatePost, input, translator.getFields())
	return r.getTag(ctx, t.ID)
}

func (r *mutationResolver) BulkTagUpdate(ctx context.Context, input BulkTagUpdateInput) ([]*models.Tag, error) {
	tagIDs, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return nil, fmt.Errorf("converting ids: %w", err)
	}

	compatInput := input
	compatInput.ApplyToItemsMatchingFilters = nil
	compatInput.FindFilter = nil
	compatInput.TagFilterAst = nil

	if _, err := r.BulkTagUpdateJob(ctx, compatInput); err != nil {
		return nil, err
	}

	return refetchBulkUpdateResults(ctx, tagIDs, r.getTag)
}

func (r *mutationResolver) TagDestroy(ctx context.Context, input TagDestroyInput) (bool, error) {
	tagID, err := strconv.Atoi(input.ID)
	if err != nil {
		return false, fmt.Errorf("converting id: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		return r.repository.Tag.Destroy(ctx, tagID)
	}); err != nil {
		return false, err
	}

	r.hookExecutor.ExecutePostHooks(ctx, tagID, hook.TagDestroyPost, input, nil)

	return true, nil
}

func (r *mutationResolver) TagsDestroy(ctx context.Context, tagIDs []string) (bool, error) {
	ids, err := stringslice.StringSliceToIntSlice(tagIDs)
	if err != nil {
		return false, fmt.Errorf("converting ids: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Tag
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
		r.hookExecutor.ExecutePostHooks(ctx, id, hook.TagDestroyPost, tagIDs, nil)
	}

	return true, nil
}

func (r *mutationResolver) TagsMerge(ctx context.Context, input TagsMergeInput) (*models.Tag, error) {
	source, err := stringslice.StringSliceToIntSlice(input.Source)
	if err != nil {
		return nil, fmt.Errorf("converting source ids: %w", err)
	}

	destination, err := strconv.Atoi(input.Destination)
	if err != nil {
		return nil, fmt.Errorf("converting destination id: %w", err)
	}

	if len(source) == 0 {
		return nil, nil
	}

	var values *models.TagPartial
	var imageData []byte

	if input.Values != nil {
		translator := changesetTranslator{
			inputMap: getNamedUpdateInputMap(ctx, "input.values"),
		}

		values, err = tagPartialFromInput(*input.Values, translator)
		if err != nil {
			return nil, err
		}

		if input.Values.Image != nil {
			var err error
			imageData, err = r.processEntityImageInput(ctx, *input.Values.Image, true)
			if err != nil {
				return nil, fmt.Errorf("processing cover image: %w", err)
			}
		}
	} else {
		v := models.NewTagPartial()
		values = &v
	}

	var t *models.Tag
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Tag

		var err error
		t, err = qb.Find(ctx, destination)
		if err != nil {
			return err
		}

		if t == nil {
			return fmt.Errorf("tag with id %d not found", destination)
		}

		if err = qb.Merge(ctx, source, destination); err != nil {
			return err
		}

		if err := tag.ValidateUpdate(ctx, destination, *values, qb); err != nil {
			return err
		}

		if _, err := qb.UpdatePartial(ctx, destination, *values); err != nil {
			return fmt.Errorf("updating tag: %w", err)
		}

		if len(imageData) > 0 {
			if err := qb.UpdateImage(ctx, destination, imageData); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	r.hookExecutor.ExecutePostHooks(ctx, t.ID, hook.TagMergePost, input, nil)

	return t, nil
}
