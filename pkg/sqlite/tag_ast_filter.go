package sqlite

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

type tagASTFilterHandler struct {
	ast *models.FilterAST
}

func (h *tagASTFilterHandler) handle(ctx context.Context, f *filterBuilder) {
	if h.ast == nil || h.ast.Root == nil {
		return
	}

	predicate, err := compileASTNode(ctx, h.ast.Root, tagASTConditionHandler)
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

func tagASTConditionHandler(condition *models.FilterASTCondition) (criterionHandler, error) {
	qb := &tagFilterHandler{}

	switch condition.Field {
	case "name":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "tags.name"), nil
	case "sort_name":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "tags.sort_name"), nil
	case "aliases":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.aliasCriterionHandler(&input), nil
	case "favorite":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return boolCriterionHandler(&input, "tags.favorite", nil), nil
	case "description":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "tags.description"), nil
	case "ignore_auto_tag":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return boolCriterionHandler(&input, "tags.ignore_auto_tag", nil), nil
	case "is_missing":
		input, err := decodeASTValue[string](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.isMissingCriterionHandler(&input), nil
	case "scene_count":
		input, err := decodeASTValue[models.HierarchicalCountInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.sceneCountCriterionHandler(&input), nil
	case "image_count":
		input, err := decodeASTValue[models.HierarchicalCountInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.imageCountCriterionHandler(&input), nil
	case "gallery_count":
		input, err := decodeASTValue[models.HierarchicalCountInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.galleryCountCriterionHandler(&input), nil
	case "performer_count":
		input, err := decodeASTValue[models.HierarchicalCountInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performerCountCriterionHandler(&input), nil
	case "studio_count":
		input, err := decodeASTValue[models.HierarchicalCountInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.studioCountCriterionHandler(&input), nil
	case "group_count":
		input, err := decodeASTValue[models.HierarchicalCountInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.groupCountCriterionHandler(&input), nil
	case "movie_count":
		input, err := decodeASTValue[models.HierarchicalCountInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.groupCountCriterionHandler(&input), nil
	case "marker_count":
		input, err := decodeASTValue[models.HierarchicalCountInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.markerCountCriterionHandler(&input), nil
	case "parents":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return tagHierarchyHandler.ParentsCriterionHandler(&input), nil
	case "children":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return tagHierarchyHandler.ChildrenCriterionHandler(&input), nil
	case "parent_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return tagHierarchyHandler.ParentCountCriterionHandler(&input), nil
	case "child_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return tagHierarchyHandler.ChildCountCriterionHandler(&input), nil
	case "stash_id_endpoint":
		input, err := decodeASTValue[models.StashIDCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &stashIDCriterionHandler{
			c:                 &input,
			stashIDRepository: &tagRepository.stashIDs,
			stashIDTableAs:    "tag_stash_ids",
			parentIDCol:       "tags.id",
		}, nil
	case "created_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "tags.created_at"}, nil
	case "updated_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "tags.updated_at"}, nil
	case "custom_fields":
		input, err := decodeASTValue[[]models.CustomFieldCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &customFieldsFilterHandler{
			table: tagsCustomFieldsTable.GetTable(),
			fkCol: tagIDColumn,
			c:     input,
			idCol: "tags.id",
		}, nil
	default:
		return nil, fmt.Errorf("tag AST condition %q is not supported yet", condition.Field)
	}
}
