package api

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

var performerMergeSafetyFieldOrder = []string{
	"disambiguation",
	"gender",
	"birthdate",
	"death_date",
	"ethnicity",
	"country",
	"eye_color",
	"hair_color",
	"height_cm",
	"weight",
	"measurements",
	"fake_tits",
	"penis_length",
	"circumcised",
	"career_start",
	"career_end",
	"tattoos",
	"piercings",
	"favorite",
	"rating100",
	"details",
	"ignore_auto_tag",
	"urls",
	"aliases",
	"stash_ids",
	"custom_fields",
	"image",
}

// validatePerformerMergeResolvedValues prevents the safe v3 merge path from
// silently discarding source-only values. A differing source value is safe
// when the field is explicitly present in values: that presence records the
// caller's keep/source/combine decision, including an intentional empty value.
//
// Names and per-name auto-tag policies are omitted from this check because
// preservePerformerMergeNames retains them automatically. Tags and entity
// relationships are omitted because PerformerStore.Merge unions them.
func validatePerformerMergeResolvedValues(
	ctx context.Context,
	repository models.PerformerReader,
	destination *models.Performer,
	sources []*models.Performer,
	valuesTranslator changesetTranslator,
) error {
	conflicts, err := performerMergeConflictingFields(ctx, repository, destination, sources)
	if err != nil {
		return err
	}

	var unresolved []string
	for _, field := range performerMergeSafetyFieldOrder {
		if conflicts[field] && !performerMergeFieldResolved(valuesTranslator, field) {
			unresolved = append(unresolved, field)
		}
	}

	if len(unresolved) > 0 {
		return fmt.Errorf(
			"performer merge would discard unresolved source values for fields: %s; provide each field in input.values",
			strings.Join(unresolved, ", "),
		)
	}

	return nil
}

func performerMergeFieldResolved(translator changesetTranslator, field string) bool {
	switch field {
	case "aliases":
		return translator.hasField("aliases") || translator.hasField("alias_list")
	case "career_start", "career_end":
		return translator.hasField(field) || translator.hasField("career_length")
	case "image":
		return translator.hasField("image") || translator.hasField("image_input")
	default:
		return translator.hasField(field)
	}
}

func performerMergeConflictingFields(
	ctx context.Context,
	repository models.PerformerReader,
	destination *models.Performer,
	sources []*models.Performer,
) (map[string]bool, error) {
	performers := make([]*models.Performer, 0, len(sources)+1)
	performers = append(performers, destination)
	performers = append(performers, sources...)

	customFields := make(map[int]map[string]interface{}, len(performers))
	for _, p := range performers {
		if err := p.LoadAliases(ctx, repository); err != nil {
			return nil, fmt.Errorf("loading aliases for performer %d before merge: %w", p.ID, err)
		}
		if err := p.LoadURLs(ctx, repository); err != nil {
			return nil, fmt.Errorf("loading URLs for performer %d before merge: %w", p.ID, err)
		}
		if err := p.LoadStashIDs(ctx, repository); err != nil {
			return nil, fmt.Errorf("loading stash IDs for performer %d before merge: %w", p.ID, err)
		}

		fields, err := repository.GetCustomFields(ctx, p.ID)
		if err != nil {
			return nil, fmt.Errorf("loading custom fields for performer %d before merge: %w", p.ID, err)
		}
		customFields[p.ID] = fields
	}

	conflicts := make(map[string]bool)
	markString := func(field, destinationValue, sourceValue string) {
		if performerMergeSourceStringDiffers(destinationValue, sourceValue) {
			conflicts[field] = true
		}
	}

	for _, source := range sources {
		markString("disambiguation", destination.Disambiguation, source.Disambiguation)
		if performerMergeSourcePtrDiffers(destination.Gender, source.Gender) {
			conflicts["gender"] = true
		}
		if performerMergeSourceDateDiffers(destination.Birthdate, source.Birthdate) {
			conflicts["birthdate"] = true
		}
		if performerMergeSourceDateDiffers(destination.DeathDate, source.DeathDate) {
			conflicts["death_date"] = true
		}
		markString("ethnicity", destination.Ethnicity, source.Ethnicity)
		markString("country", destination.Country, source.Country)
		markString("eye_color", destination.EyeColor, source.EyeColor)
		markString("hair_color", destination.HairColor, source.HairColor)
		if performerMergeSourcePtrDiffers(destination.Height, source.Height) {
			conflicts["height_cm"] = true
		}
		if performerMergeSourcePtrDiffers(destination.Weight, source.Weight) {
			conflicts["weight"] = true
		}
		markString("measurements", destination.Measurements, source.Measurements)
		markString("fake_tits", destination.FakeTits, source.FakeTits)
		if performerMergeSourcePtrDiffers(destination.PenisLength, source.PenisLength) {
			conflicts["penis_length"] = true
		}
		if performerMergeSourcePtrDiffers(destination.Circumcised, source.Circumcised) {
			conflicts["circumcised"] = true
		}
		if performerMergeSourceDateDiffers(destination.CareerStart, source.CareerStart) {
			conflicts["career_start"] = true
		}
		if performerMergeSourceDateDiffers(destination.CareerEnd, source.CareerEnd) {
			conflicts["career_end"] = true
		}
		markString("tattoos", destination.Tattoos, source.Tattoos)
		markString("piercings", destination.Piercings, source.Piercings)
		if source.Favorite && !destination.Favorite {
			conflicts["favorite"] = true
		}
		if performerMergeSourcePtrDiffers(destination.Rating, source.Rating) {
			conflicts["rating100"] = true
		}
		markString("details", destination.Details, source.Details)
		if source.IgnoreAutoTag && !destination.IgnoreAutoTag {
			conflicts["ignore_auto_tag"] = true
		}
	}

	if performerMergeHasSourceURL(destination, sources) {
		conflicts["urls"] = true
	}
	if performerMergeHasSourceAlias(destination, sources) {
		conflicts["aliases"] = true
	}
	if performerMergeHasSourceStashID(destination, sources) {
		conflicts["stash_ids"] = true
	}
	if performerMergeHasSourceCustomField(customFields[destination.ID], sources, customFields) {
		conflicts["custom_fields"] = true
	}

	for _, source := range sources {
		hasImage, err := repository.HasImage(ctx, source.ID)
		if err != nil {
			return nil, fmt.Errorf("checking image for performer %d before merge: %w", source.ID, err)
		}
		if hasImage {
			conflicts["image"] = true
		}
	}

	return conflicts, nil
}

func performerMergeSourceStringDiffers(destination, source string) bool {
	source = strings.TrimSpace(source)
	return source != "" && source != strings.TrimSpace(destination)
}

func performerMergeSourcePtrDiffers[T comparable](destination, source *T) bool {
	return source != nil && (destination == nil || *source != *destination)
}

func performerMergeSourceDateDiffers(destination, source *models.Date) bool {
	return source != nil && (destination == nil || source.String() != destination.String())
}

func performerMergeHasSourceURL(destination *models.Performer, sources []*models.Performer) bool {
	destinationURLs := make(map[string]bool)
	for _, value := range destination.URLs.List() {
		destinationURLs[strings.TrimSpace(value)] = true
	}

	for _, source := range sources {
		for _, value := range source.URLs.List() {
			value = strings.TrimSpace(value)
			if value != "" && !destinationURLs[value] {
				return true
			}
		}
	}

	return false
}

func performerMergeHasSourceAlias(destination *models.Performer, sources []*models.Performer) bool {
	destinationAliases := make(map[string]bool)
	for _, value := range destination.Aliases.List() {
		destinationAliases[strings.ToLower(strings.TrimSpace(value.Alias))] = true
	}

	for _, source := range sources {
		for _, value := range source.Aliases.List() {
			key := strings.ToLower(strings.TrimSpace(value.Alias))
			if key != "" && !destinationAliases[key] {
				return true
			}
		}
	}

	return false
}

func performerMergeHasSourceStashID(destination *models.Performer, sources []*models.Performer) bool {
	destinationStashIDs := make(map[string]string)
	for _, value := range destination.StashIDs.List() {
		destinationStashIDs[value.Endpoint] = value.StashID
	}

	for _, source := range sources {
		for _, value := range source.StashIDs.List() {
			destinationStashID, exists := destinationStashIDs[value.Endpoint]
			if !exists || destinationStashID != value.StashID {
				return true
			}
		}
	}

	return false
}

func performerMergeHasSourceCustomField(
	destination map[string]interface{},
	sources []*models.Performer,
	customFields map[int]map[string]interface{},
) bool {
	for _, source := range sources {
		for key, sourceValue := range customFields[source.ID] {
			destinationValue, exists := destination[key]
			if !exists || !reflect.DeepEqual(destinationValue, sourceValue) {
				return true
			}
		}
	}

	return false
}
