package sqlite

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

type studioASTFilterHandler struct {
	ast *models.FilterAST
}

func (h *studioASTFilterHandler) handle(ctx context.Context, f *filterBuilder) {
	if h.ast == nil || h.ast.Root == nil {
		return
	}

	predicate, err := compileASTNode(ctx, h.ast.Root, studioASTConditionHandler)
	if err != nil {
		f.setError(err)
		return
	}

	if predicate == nil {
		return
	}

	built := predicate.toFilterBuilder()
	*f = *built
}

func studioASTConditionHandler(condition *models.FilterASTCondition) (criterionHandler, error) {
	qb := &studioFilterHandler{}

	switch condition.Field {
	case "name":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "studios.name"), nil
	case "details":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "studios.details"), nil
	case "url":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.urlsCriterionHandler(&input), nil
	case "rating100":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return intCriterionHandler(&input, "studios.rating", nil), nil
	case "favorite":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return boolCriterionHandler(&input, "studios.favorite", nil), nil
	case "ignore_auto_tag":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return boolCriterionHandler(&input, "studios.ignore_auto_tag", nil), nil
	case "organized":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return boolCriterionHandler(&input, "studios.organized", nil), nil
	case "stash_id_endpoint":
		input, err := decodeASTValue[models.StashIDCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &stashIDCriterionHandler{
			c:                 &input,
			stashIDRepository: &studioRepository.stashIDs,
			stashIDTableAs:    "studio_stash_ids",
			parentIDCol:       "studios.id",
		}, nil
	case "is_missing":
		input, err := decodeASTValue[string](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.isMissingCriterionHandler(&input), nil
	case "tag_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.tagCountCriterionHandler(&input), nil
	case "scene_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.sceneCountCriterionHandler(&input), nil
	case "image_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.imageCountCriterionHandler(&input), nil
	case "gallery_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.galleryCountCriterionHandler(&input), nil
	case "group_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.groupCountCriterionHandler(&input), nil
	case "parents":
		input, err := decodeASTValue[models.MultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.parentCriterionHandler(&input), nil
	case "aliases":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.aliasCriterionHandler(&input), nil
	case "tags":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.tagsCriterionHandler(&input), nil
	case "ancestor_tags":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.ancestorTagsCriterionHandler(&input), nil
	case "descendant_tags":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.descendantTagsCriterionHandler(&input), nil
	case "child_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.childCountCriterionHandler(&input), nil
	case "created_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "studios.created_at"}, nil
	case "updated_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "studios.updated_at"}, nil
	case "custom_fields":
		input, err := decodeASTValue[[]models.CustomFieldCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &customFieldsFilterHandler{
			table: studiosCustomFieldsTable.GetTable(),
			fkCol: studioIDColumn,
			c:     input,
			idCol: "studios.id",
		}, nil
	default:
		return nil, fmt.Errorf("studio AST condition %q is not supported yet", condition.Field)
	}
}
