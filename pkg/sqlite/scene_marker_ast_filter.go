package sqlite

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

type sceneMarkerASTFilterHandler struct {
	ast *models.FilterAST
}

func (h *sceneMarkerASTFilterHandler) handle(ctx context.Context, f *filterBuilder) {
	if h.ast == nil || h.ast.Root == nil {
		return
	}

	predicate, err := compileASTNode(ctx, h.ast.Root, sceneMarkerASTConditionHandler)
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

func sceneMarkerASTConditionHandler(condition *models.FilterASTCondition) (criterionHandler, error) {
	qb := &sceneMarkerFilterHandler{}

	switch condition.Field {
	case "tags":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.tagsCriterionHandler(&input), nil
	case "scene_tags":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.sceneTagsCriterionHandler(&input), nil
	case "performers":
		input, err := decodeASTValue[models.MultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performersCriterionHandler(&input), nil
	case "scenes":
		input, err := decodeASTValue[models.MultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.scenesCriterionHandler(&input), nil
	case "duration":
		input, err := decodeASTValue[models.FloatCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return floatCriterionHandler(&input, "COALESCE(scene_markers.end_seconds - scene_markers.seconds, NULL)", nil), nil
	case "created_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "scene_markers.created_at"}, nil
	case "updated_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "scene_markers.updated_at"}, nil
	case "scene_date":
		input, err := decodeASTValue[models.DateCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &dateCriterionHandler{c: &input, column: "scenes.date", joinFn: qb.joinScenes}, nil
	case "scene_created_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "scenes.created_at", joinFn: qb.joinScenes}, nil
	case "scene_updated_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "scenes.updated_at", joinFn: qb.joinScenes}, nil
	default:
		return nil, fmt.Errorf("scene marker AST condition %q is not supported yet", condition.Field)
	}
}
