package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/performer"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sliceutil"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
	"github.com/stashapp/stash/pkg/utils"
)

const (
	twitterURL   = "https://twitter.com"
	instagramURL = "https://instagram.com"
)

// used to refetch performer after hooks run
func (r *mutationResolver) getPerformer(ctx context.Context, id int) (ret *models.Performer, err error) {
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.Performer.Find(ctx, id)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *mutationResolver) PerformerCreate(ctx context.Context, input models.PerformerCreateInput) (*models.Performer, error) {
	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	// Populate a new performer from the input
	newPerformer := models.NewPerformer()

	newPerformer.Name = strings.TrimSpace(input.Name)
	newPerformer.Disambiguation = translator.string(input.Disambiguation)
	var aliases []models.PerformerAlias
	if input.Aliases != nil {
		for _, a := range input.Aliases {
			aliases = append(aliases, models.PerformerAlias{
				Alias:         a.Alias,
				IgnoreAutoTag: a.IgnoreAutoTag,
			})
		}
	} else if input.AliasList != nil {
		for _, a := range input.AliasList {
			aliases = append(aliases, models.PerformerAlias{Alias: a, IgnoreAutoTag: true})
		}
	}
	newPerformer.Aliases = models.NewRelatedPerformerAliases(performer.NormalizeAliases(newPerformer.Name, aliases))
	newPerformer.Gender = input.Gender
	newPerformer.Ethnicity = translator.string(input.Ethnicity)
	newPerformer.Country = translator.string(input.Country)
	newPerformer.EyeColor = translator.string(input.EyeColor)
	newPerformer.Measurements = translator.string(input.Measurements)
	newPerformer.FakeTits = translator.string(input.FakeTits)
	newPerformer.PenisLength = input.PenisLength
	newPerformer.Circumcised = input.Circumcised
	newPerformer.Tattoos = translator.string(input.Tattoos)
	newPerformer.Piercings = translator.string(input.Piercings)
	newPerformer.Favorite = translator.bool(input.Favorite)
	newPerformer.Rating = input.Rating100
	newPerformer.Details = translator.string(input.Details)
	newPerformer.HairColor = translator.string(input.HairColor)
	newPerformer.Height = input.HeightCm
	newPerformer.Weight = input.Weight
	newPerformer.IgnoreAutoTag = translator.bool(input.IgnoreAutoTag)
	newPerformer.IgnorePrimaryNameAutoTag = translator.bool(input.IgnorePrimaryNameAutoTag)
	newPerformer.StashIDs = models.NewRelatedStashIDs(models.StashIDInputs(input.StashIds).ToStashIDs())

	newPerformer.URLs = models.NewRelatedStrings([]string{})
	if input.URL != nil {
		newPerformer.URLs.Add(strings.TrimSpace(*input.URL))
	}
	if input.Twitter != nil {
		newPerformer.URLs.Add(utils.URLFromHandle(strings.TrimSpace(*input.Twitter), twitterURL))
	}
	if input.Instagram != nil {
		newPerformer.URLs.Add(utils.URLFromHandle(strings.TrimSpace(*input.Instagram), instagramURL))
	}

	if input.Urls != nil {
		newPerformer.URLs.Add(stringslice.TrimSpace(input.Urls)...)
	}

	var err error

	newPerformer.Birthdate, err = translator.datePtr(input.Birthdate)
	if err != nil {
		return nil, fmt.Errorf("converting birthdate: %w", err)
	}
	newPerformer.DeathDate, err = translator.datePtr(input.DeathDate)
	if err != nil {
		return nil, fmt.Errorf("converting death date: %w", err)
	}

	newPerformer.CareerStart, err = translator.datePtr(input.CareerStart)
	if err != nil {
		return nil, fmt.Errorf("converting career start: %w", err)
	}
	newPerformer.CareerEnd, err = translator.datePtr(input.CareerEnd)
	if err != nil {
		return nil, fmt.Errorf("converting career end: %w", err)
	}

	// if career_start/career_end not provided, parse deprecated career_length
	if newPerformer.CareerStart == nil && newPerformer.CareerEnd == nil && input.CareerLength != nil {
		start, end, err := models.ParseYearRangeString(*input.CareerLength)
		if err != nil {
			return nil, fmt.Errorf("could not parse career_length %q: %w", *input.CareerLength, err)
		}
		newPerformer.CareerStart = start
		newPerformer.CareerEnd = end
	}

	newPerformer.TagIDs, err = translator.relatedIds(input.TagIds)
	if err != nil {
		return nil, fmt.Errorf("converting tag ids: %w", err)
	}

	var imageData []byte
	if input.Image != nil || input.ImageInput != nil {
		imageData, _, err = r.processEntityImageFields(ctx, input.Image, input.ImageInput)
		if err != nil {
			return nil, err
		}
	}

	// Start the transaction and save the performer
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Performer

		if err := performer.ValidateCreate(newPerformer); err != nil {
			return err
		}

		i := &models.CreatePerformerInput{
			Performer: &newPerformer,
			// convert json.Numbers to int/float
			CustomFields: convertMapJSONNumbers(input.CustomFields),
		}

		err = qb.Create(ctx, i)
		if err != nil {
			return err
		}

		// update image table
		if len(imageData) > 0 {
			if err := qb.UpdateImage(ctx, newPerformer.ID, imageData); err != nil {
				return err
			}
		}

		return r.recordProviderSelections(ctx, models.ArchivePerformer, newPerformer.ID, input.ProviderMetadata, translator)
	}); err != nil {
		return nil, err
	}

	r.hookExecutor.ExecutePostHooks(ctx, newPerformer.ID, hook.PerformerCreatePost, input, nil)
	return r.getPerformer(ctx, newPerformer.ID)
}

func validateNoLegacyURLs(translator changesetTranslator) error {
	// ensure url/twitter/instagram are not included in the input
	if translator.hasField("url") {
		return fmt.Errorf("url field must not be included if urls is included")
	}
	if translator.hasField("twitter") {
		return fmt.Errorf("twitter field must not be included if urls is included")
	}
	if translator.hasField("instagram") {
		return fmt.Errorf("instagram field must not be included if urls is included")
	}

	return nil
}

func (r *mutationResolver) handleLegacyURLs(ctx context.Context, performerID int, changes performer.URLUpdate, partial *models.PerformerPartial) error {
	return performer.ApplyUpdateURLs(ctx, r.repository.Performer, performerID, changes, partial)
}

func legacyPerformerURLsFromInput(input models.PerformerUpdateInput, translator changesetTranslator) performer.URLUpdate {
	return performer.URLUpdate{
		URL:       translator.optionalString(input.URL, "url"),
		Twitter:   translator.optionalString(input.Twitter, "twitter"),
		Instagram: translator.optionalString(input.Instagram, "instagram"),
	}
}

func performerPartialFromInput(input models.PerformerUpdateInput, translator changesetTranslator) (*models.PerformerPartial, error) {
	// Populate performer from the input
	updatedPerformer := models.NewPerformerPartial()

	updatedPerformer.Name = translator.optionalString(input.Name, "name")
	updatedPerformer.Disambiguation = translator.optionalString(input.Disambiguation, "disambiguation")
	updatedPerformer.Gender = translator.optionalString((*string)(input.Gender), "gender")
	updatedPerformer.Ethnicity = translator.optionalString(input.Ethnicity, "ethnicity")
	updatedPerformer.Country = translator.optionalString(input.Country, "country")
	updatedPerformer.EyeColor = translator.optionalString(input.EyeColor, "eye_color")
	updatedPerformer.Measurements = translator.optionalString(input.Measurements, "measurements")
	updatedPerformer.FakeTits = translator.optionalString(input.FakeTits, "fake_tits")
	updatedPerformer.PenisLength = translator.optionalFloat64(input.PenisLength, "penis_length")
	updatedPerformer.Circumcised = translator.optionalString((*string)(input.Circumcised), "circumcised")
	// prefer career_start/career_end over deprecated career_length
	if translator.hasField("career_start") || translator.hasField("career_end") {
		var err error
		updatedPerformer.CareerStart, err = translator.optionalDate(input.CareerStart, "career_start")
		if err != nil {
			return nil, fmt.Errorf("converting career start: %w", err)
		}
		updatedPerformer.CareerEnd, err = translator.optionalDate(input.CareerEnd, "career_end")
		if err != nil {
			return nil, fmt.Errorf("converting career end: %w", err)
		}
	} else if translator.hasField("career_length") && input.CareerLength != nil {
		start, end, err := models.ParseYearRangeString(*input.CareerLength)
		if err != nil {
			return nil, fmt.Errorf("could not parse career_length %q: %w", *input.CareerLength, err)
		}
		if start != nil {
			updatedPerformer.CareerStart = models.NewOptionalDate(*start)
		}
		if end != nil {
			updatedPerformer.CareerEnd = models.NewOptionalDate(*end)
		}
	}
	updatedPerformer.Tattoos = translator.optionalString(input.Tattoos, "tattoos")
	updatedPerformer.Piercings = translator.optionalString(input.Piercings, "piercings")
	updatedPerformer.Favorite = translator.optionalBool(input.Favorite, "favorite")
	updatedPerformer.Rating = translator.optionalInt(input.Rating100, "rating100")
	updatedPerformer.Details = translator.optionalString(input.Details, "details")
	updatedPerformer.HairColor = translator.optionalString(input.HairColor, "hair_color")
	updatedPerformer.Weight = translator.optionalInt(input.Weight, "weight")
	updatedPerformer.IgnoreAutoTag = translator.optionalBool(input.IgnoreAutoTag, "ignore_auto_tag")
	updatedPerformer.IgnorePrimaryNameAutoTag = translator.optionalBool(input.IgnorePrimaryNameAutoTag, "ignore_primary_name_auto_tag")
	updatedPerformer.StashIDs = translator.updateStashIDs(input.StashIds, "stash_ids")

	var err error

	if translator.hasField("urls") {
		// ensure url/twitter/instagram are not included in the input
		if err := validateNoLegacyURLs(translator); err != nil {
			return nil, err
		}

		updatedPerformer.URLs = translator.updateStrings(input.Urls, "urls")
	}

	updatedPerformer.Birthdate, err = translator.optionalDate(input.Birthdate, "birthdate")
	if err != nil {
		return nil, fmt.Errorf("converting birthdate: %w", err)
	}
	updatedPerformer.DeathDate, err = translator.optionalDate(input.DeathDate, "death_date")
	if err != nil {
		return nil, fmt.Errorf("converting death date: %w", err)
	}

	// prefer height_cm over height
	if translator.hasField("height_cm") {
		updatedPerformer.Height = translator.optionalInt(input.HeightCm, "height_cm")
	}

	// prefer aliases over alias_list
	if translator.hasField("aliases") {
		var aliases []models.PerformerAlias
		for _, a := range input.Aliases.Values {
			aliases = append(aliases, models.PerformerAlias{
				Alias:         strings.TrimSpace(a.Alias),
				IgnoreAutoTag: a.IgnoreAutoTag,
			})
		}
		updatedPerformer.Aliases = &models.UpdatePerformerAliases{
			Values: aliases,
			Mode:   input.Aliases.Mode,
		}
	} else if translator.hasField("alias_list") {
		var aliases []models.PerformerAlias
		for _, a := range input.AliasList {
			aliases = append(aliases, models.PerformerAlias{Alias: a, IgnoreAutoTag: true})
		}
		updatedPerformer.Aliases = &models.UpdatePerformerAliases{
			Values: aliases,
			Mode:   models.RelationshipUpdateModeSet,
		}
	}

	updatedPerformer.TagIDs, err = translator.updateIds(input.TagIds, "tag_ids")
	if err != nil {
		return nil, fmt.Errorf("converting tag ids: %w", err)
	}

	updatedPerformer.CustomFields = handleUpdateCustomFields(input.CustomFields)

	return &updatedPerformer, nil
}

type performerUpdateInputResolver struct{ *Resolver }

func (r *performerUpdateInputResolver) Aliases(ctx context.Context, obj *models.PerformerUpdateInput, data []*models.PerformerAliasInput) error {
	obj.Aliases = &models.UpdatePerformerAliasesInput{
		Values: data,
		Mode:   models.RelationshipUpdateModeSet, // Use Set mode for simple aliases property, though client can also use AliasList
	}
	return nil
}

func (r *Resolver) PerformerUpdateInput() PerformerUpdateInputResolver {
	return &performerUpdateInputResolver{r}
}

func (r *mutationResolver) PerformerUpdate(ctx context.Context, input models.PerformerUpdateInput) (*models.Performer, error) {
	performerID, err := strconv.Atoi(input.ID)
	if err != nil {
		return nil, fmt.Errorf("converting id: %w", err)
	}

	translator := changesetTranslator{
		inputMap: getUpdateInputMap(ctx),
	}

	updatedPerformer, err := performerPartialFromInput(input, translator)
	if err != nil {
		return nil, err
	}

	legacyURLs := legacyPerformerURLsFromInput(input, translator)

	var imageData []byte
	imageData, imageIncluded, err := r.processEntityImageFields(ctx, input.Image, input.ImageInput)
	if err != nil {
		return nil, err
	}
	if !imageIncluded && (translator.hasField("image") || translator.hasField("image_input")) {
		imageIncluded = true
	}

	// Start the transaction and save the performer
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Performer

		if legacyURLs.AnySet() {
			if err := r.handleLegacyURLs(ctx, performerID, legacyURLs, updatedPerformer); err != nil {
				return err
			}
		}

		if updatedPerformer.Aliases != nil {
			p, err := qb.Find(ctx, performerID)
			if err != nil {
				return err
			}
			if p != nil {
				if err := p.LoadAliases(ctx, qb); err != nil {
					return err
				}

				// Preserve IgnoreAutoTag state when updating aliases via AliasList (which sets them to true by default)
				// if they already existed.
				preserveIgnore := translator.hasField("alias_list") && !translator.hasField("aliases")
				effectiveAliases := performer.GetEffectiveAliases(p.Aliases.List(), updatedPerformer.Aliases.Values, updatedPerformer.Aliases.Mode, preserveIgnore)

				name := p.Name
				if updatedPerformer.Name.Set {
					name = updatedPerformer.Name.Value
				}

				updatedPerformer.Aliases.Values = performer.NormalizeAliases(name, effectiveAliases)
				updatedPerformer.Aliases.Mode = models.RelationshipUpdateModeSet
			}
		}
		if err := performer.ValidateUpdate(ctx, performerID, *updatedPerformer, qb); err != nil {
			return err
		}

		_, err = qb.UpdatePartial(ctx, performerID, *updatedPerformer)
		if err != nil {
			return err
		}

		// update image table
		if imageIncluded {
			if err := qb.UpdateImage(ctx, performerID, imageData); err != nil {
				return err
			}
		}

		return r.recordProviderSelections(ctx, models.ArchivePerformer, performerID, input.ProviderMetadata, translator)
	}); err != nil {
		return nil, err
	}

	r.hookExecutor.ExecutePostHooks(ctx, performerID, hook.PerformerUpdatePost, input, translator.getFields())
	return r.getPerformer(ctx, performerID)
}

func (r *mutationResolver) PerformerDestroy(ctx context.Context, input PerformerDestroyInput) (bool, error) {
	id, err := strconv.Atoi(input.ID)
	if err != nil {
		return false, fmt.Errorf("converting id: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		return r.repository.Performer.Destroy(ctx, id)
	}); err != nil {
		return false, err
	}

	r.hookExecutor.ExecutePostHooks(ctx, id, hook.PerformerDestroyPost, input, nil)

	return true, nil
}

func (r *mutationResolver) PerformersDestroy(ctx context.Context, performerIDs []string) (bool, error) {
	ids, err := stringslice.StringSliceToIntSlice(performerIDs)
	if err != nil {
		return false, fmt.Errorf("converting ids: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Performer
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
		r.hookExecutor.ExecutePostHooks(ctx, id, hook.PerformerDestroyPost, performerIDs, nil)
	}

	return true, nil
}

func (r *mutationResolver) PerformerMerge(ctx context.Context, input PerformerMergeInput) (*models.Performer, error) {
	srcIDs, err := stringslice.StringSliceToIntSlice(input.Source)
	if err != nil {
		return nil, fmt.Errorf("converting source ids: %w", err)
	}

	// ensure source ids are unique
	srcIDs = sliceutil.AppendUniques(nil, srcIDs)

	destID, err := strconv.Atoi(input.Destination)
	if err != nil {
		return nil, fmt.Errorf("converting destination id: %w", err)
	}

	// ensure destination is not in source list
	if slices.Contains(srcIDs, destID) {
		return nil, errors.New("destination performer cannot be in source list")
	}

	requireResolvedValues := input.RequireResolvedValues != nil && *input.RequireResolvedValues
	if requireResolvedValues && len(srcIDs) == 0 {
		return nil, errors.New("safe performer merge requires at least one source performer")
	}
	if requireResolvedValues && input.Values != nil {
		valuesID, err := strconv.Atoi(input.Values.ID)
		if err != nil || valuesID != destID {
			return nil, errors.New("performer merge values id must match destination id")
		}
	}

	var values *models.PerformerPartial
	var imageData []byte
	var imageIncluded bool
	var imageSourceID *int
	var legacyAliasList bool
	var resolvedTagIDs *models.UpdateIDs
	valuesTranslator := changesetTranslator{}

	if input.Values != nil {
		if len(input.Values.ProviderMetadata) != 0 {
			return nil, fmt.Errorf("%w: provider selections must be saved separately from a merge", models.ErrProviderMetadataInvalid)
		}
		valuesTranslator = changesetTranslator{
			inputMap: getNamedUpdateInputMap(ctx, "input.values"),
		}

		values, err = performerPartialFromInput(*input.Values, valuesTranslator)
		if err != nil {
			return nil, err
		}
		legacyAliasList = valuesTranslator.hasField("alias_list") && !valuesTranslator.hasField("aliases")
		legacyURLs := legacyPerformerURLsFromInput(*input.Values, valuesTranslator)
		if legacyURLs.AnySet() {
			return nil, errors.New("merging legacy performer URLs is not supported")
		}

		if image := input.Values.ImageInput; image != nil && image.PerformerID != nil {
			if input.Values.Image != nil {
				return nil, errors.New("image and image_input cannot both be set")
			}
			if err := validateEntityImageSource(image); err != nil {
				return nil, err
			}
			id, err := strconv.Atoi(*image.PerformerID)
			if err != nil || (id != destID && !slices.Contains(srcIDs, id)) {
				return nil, errors.New("merge image must belong to a source or destination performer")
			}
			// Read the selected image in the merge transaction, before deleting
			// sources. The typed reference has already passed UUID validation.
			imageSourceID = &id
		}
		if imageSourceID == nil {
			imageData, imageIncluded, err = r.processEntityImageFields(ctx, input.Values.Image, input.Values.ImageInput)
			if err != nil {
				return nil, fmt.Errorf("processing performer image: %w", err)
			}
		} else {
			imageIncluded = true
		}
	} else {
		v := models.NewPerformerPartial()
		values = &v
	}

	var dest *models.Performer
	var notification *PerformerMergeNotification
	if err := r.withTxn(ctx, func(ctx context.Context) error {
		qb := r.repository.Performer

		dest, err = qb.Find(ctx, destID)
		if err != nil {
			return fmt.Errorf("finding destination performer ID %d: %w", destID, err)
		}
		if dest == nil {
			return fmt.Errorf("destination performer ID %d not found", destID)
		}

		// ensure source performers exist
		sources, err := qb.FindMany(ctx, srcIDs)
		if err != nil {
			return fmt.Errorf("finding source performers: %w", err)
		}
		if requireResolvedValues {
			if err := validatePerformerMergeResolvedValues(ctx, qb, dest, sources, valuesTranslator); err != nil {
				return err
			}
		}
		if err := preservePerformerMergeNames(ctx, qb, dest, sources, values, legacyAliasList); err != nil {
			return err
		}
		notification, err = r.capturePerformerMerge(ctx, dest, sources)
		if err != nil {
			return err
		}

		if imageSourceID != nil {
			imageData, err = qb.GetImage(ctx, *imageSourceID)
			if err != nil {
				return fmt.Errorf("reading source performer image: %w", err)
			}
		}

		if requireResolvedValues {
			if err := performer.ValidateUpdate(ctx, destID, *values, qb); err != nil {
				return fmt.Errorf("validating merged performer: %w", err)
			}

			// PerformerStore.Merge always unions source tags. Defer an explicit
			// safe-mode tag resolution until after that transfer so keep/source
			// choices produce exactly the set shown by the v3 preview.
			resolvedTagIDs = values.TagIDs
			values.TagIDs = nil
		}

		dest, err = qb.UpdatePartial(ctx, destID, *values)
		if err != nil {
			return fmt.Errorf("updating performer: %w", err)
		}

		if err := qb.Merge(ctx, srcIDs, destID); err != nil {
			return fmt.Errorf("merging performers: %w", err)
		}

		if resolvedTagIDs != nil {
			resolvedTags := models.NewPerformerPartial()
			resolvedTags.TagIDs = resolvedTagIDs
			dest, err = qb.UpdatePartial(ctx, destID, resolvedTags)
			if err != nil {
				return fmt.Errorf("applying resolved performer tags: %w", err)
			}
		}

		if imageIncluded {
			if err := qb.UpdateImage(ctx, destID, imageData); err != nil {
				return err
			}
		}
		if notification != nil {
			notification.Destination, err = r.performerMergeProfile(ctx, dest)
			if err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	if notification != nil {
		r.hookExecutor.ExecutePostHooks(ctx, destID, hook.PerformerMergePost, *notification, nil)
	}
	return dest, nil
}
