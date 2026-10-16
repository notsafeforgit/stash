package sqlite

import (
	"fmt"
	"reflect"

	"github.com/stashapp/stash/pkg/models"
)

// Studio tags use the same hierarchy and exclusion semantics for each entity
// with a primary studio. A negative criterion also retains unassigned entities.
func studioTagsASTCriterionHandler(table string, value any) (criterionHandler, error) {
	input, err := decodeASTValue[models.StudioTagFilterInput](value)
	if err != nil {
		return nil, err
	}
	studioFilter, err := studioTagFilterToStudioFilter(input)
	if err != nil {
		return nil, err
	}
	return &relatedFilterHandler{
		relatedIDCol:          table + ".studio_id",
		relatedRepo:           studioRepository.repository,
		relatedHandler:        &studioFilterHandler{studioFilter},
		includeMissingRelated: input.Modifier == models.CriterionModifierIsNull || relatedFilterIncludesMissingRelation(studioFilter),
	}, nil
}

func studioTagFilterToStudioFilter(input models.StudioTagFilterInput) (*models.StudioFilterType, error) {
	mode := input.HierarchyMode
	if mode == "" {
		mode = models.StudioTagHierarchyModeExact
	}
	if !mode.IsValid() {
		return nil, fmt.Errorf("invalid studio tag hierarchy mode %q", mode)
	}

	criterion := input.HierarchicalMultiCriterionInput
	active, excludeOnly, _ := criterionIsExcludeOnly("HierarchicalMultiCriterionInput", reflect.ValueOf(criterion))
	operator := models.FilterGroupOperatorOr
	// "No tags" must hold throughout the chosen studio hierarchy, just as
	// excluded tags must be absent throughout it.
	if input.Modifier == models.CriterionModifierIsNull || (active && excludeOnly) {
		operator = models.FilterGroupOperatorAnd
	}

	root := &models.StudioFilterType{
		Tags: &criterion,
	}
	if mode == models.StudioTagHierarchyModeExact {
		return root, nil
	}

	ancestorFilter := &models.StudioFilterType{
		AncestorTags: &criterion,
	}
	if mode == models.StudioTagHierarchyModeAncestors {
		applyStudioFilterOperator(root, operator, ancestorFilter)
		return root, nil
	}

	descendantFilter := &models.StudioFilterType{
		DescendantTags: &criterion,
	}
	if mode == models.StudioTagHierarchyModeDescendants {
		applyStudioFilterOperator(root, operator, descendantFilter)
		return root, nil
	}

	applyStudioFilterOperator(ancestorFilter, operator, descendantFilter)
	applyStudioFilterOperator(root, operator, ancestorFilter)
	return root, nil
}

func applyStudioFilterOperator(target *models.StudioFilterType, operator models.FilterGroupOperator, nested *models.StudioFilterType) {
	if operator == models.FilterGroupOperatorAnd {
		target.And = nested
		return
	}

	target.Or = nested
}
