package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/performer"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sliceutil/stringslice"
)

// Fork bulk operations. Legacy resolvers delegate here with explicit IDs;
// only the additive job mutations may select items by filter.

type performerBulkUpdateOperation struct {
	repository       models.PerformerReaderWriter
	updatedPerformer models.PerformerPartial
	legacyURLs       legacyPerformerURLs
}

func (o performerBulkUpdateOperation) Update(ctx context.Context, id int) error {
	if o.legacyURLs.AnySet() {
		if err := handleLegacyPerformerURLs(ctx, o.repository, id, o.legacyURLs, &o.updatedPerformer); err != nil {
			return err
		}
	}

	if err := performer.ValidateUpdate(ctx, id, o.updatedPerformer, o.repository); err != nil {
		return err
	}

	_, err := o.repository.UpdatePartial(ctx, id, o.updatedPerformer)
	return err
}

func (r *mutationResolver) BulkPerformerUpdateJob(ctx context.Context, input BulkPerformerUpdateInput) (string, error) {
	performerIDs, err := stringslice.StringSliceToIntSlice(input.Ids)
	if err != nil {
		return "", fmt.Errorf("converting ids: %w", err)
	}

	useBackgroundJob := input.ApplyToItemsMatchingFilters != nil && *input.ApplyToItemsMatchingFilters
	if useBackgroundJob {
		if !hasBulkUpdateFilter(input.FindFilter, input.PerformerFilterAst) {
			return "", fmt.Errorf("performer_filter_ast or find_filter.q is required when apply_to_items_matching_filters is true")
		}

		findFilter := sanitizeBulkUpdateFindFilter(input.FindFilter)
		err = r.withReadTxn(ctx, func(ctx context.Context) error {
			result, _, qErr := r.repository.Performer.QueryAST(ctx, input.PerformerFilterAst, findFilter)
			if qErr != nil {
				return qErr
			}

			performerIDs = idsFromItems(result, func(item *models.Performer) int {
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

	// Populate performer from the input
	updatedPerformer := models.NewPerformerPartial()

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
		updatedPerformer.CareerStart, err = translator.optionalDate(input.CareerStart, "career_start")
		if err != nil {
			return "", fmt.Errorf("converting career start: %w", err)
		}
		updatedPerformer.CareerEnd, err = translator.optionalDate(input.CareerEnd, "career_end")
		if err != nil {
			return "", fmt.Errorf("converting career end: %w", err)
		}
	} else if translator.hasField("career_length") && input.CareerLength != nil {
		start, end, err := models.ParseYearRangeString(*input.CareerLength)
		if err != nil {
			return "", fmt.Errorf("could not parse career_length %q: %w", *input.CareerLength, err)
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

	if translator.hasField("urls") {
		// ensure url/twitter/instagram are not included in the input
		if err := validateNoLegacyURLs(translator); err != nil {
			return "", err
		}

		updatedPerformer.URLs = translator.updateStringsBulk(input.Urls, "urls")
	}

	legacyURLs := legacyPerformerURLs{
		URL:       translator.optionalString(input.URL, "url"),
		Twitter:   translator.optionalString(input.Twitter, "twitter"),
		Instagram: translator.optionalString(input.Instagram, "instagram"),
	}

	updatedPerformer.Birthdate, err = translator.optionalDate(input.Birthdate, "birthdate")
	if err != nil {
		return "", fmt.Errorf("converting birthdate: %w", err)
	}
	updatedPerformer.DeathDate, err = translator.optionalDate(input.DeathDate, "death_date")
	if err != nil {
		return "", fmt.Errorf("converting death date: %w", err)
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
		for _, a := range input.AliasList.Values {
			aliases = append(aliases, models.PerformerAlias{Alias: a, IgnoreAutoTag: true})
		}
		updatedPerformer.Aliases = &models.UpdatePerformerAliases{
			Values: aliases,
			Mode:   input.AliasList.Mode,
		}
	}

	updatedPerformer.TagIDs, err = translator.updateIdsBulk(input.TagIds, "tag_ids")
	if err != nil {
		return "", fmt.Errorf("converting tag ids: %w", err)
	}

	if input.CustomFields != nil {
		updatedPerformer.CustomFields = handleUpdateCustomFields(*input.CustomFields)
	}

	operation := performerBulkUpdateOperation{
		repository:       r.repository.Performer,
		updatedPerformer: updatedPerformer,
		legacyURLs:       legacyURLs,
	}

	if !useBackgroundJob {
		if err := r.withTxn(ctx, func(ctx context.Context) error {
			for _, performerID := range performerIDs {
				if err := operation.Update(ctx, performerID); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return "", err
		}

		for _, performerID := range performerIDs {
			r.hookExecutor.ExecutePostHooks(ctx, performerID, hook.PerformerUpdatePost, input, translator.getFields())
		}

		return "sync", nil
	}

	jobID := r.enqueueBulkUpdate(ctx, "Bulk Performer Update", performerIDs, operation, hook.PerformerUpdatePost, input, translator.getFields())

	return strconv.Itoa(jobID), nil
}
