package sqlite

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"
)

type performerASTFilterHandler struct {
	ast *models.FilterAST
}

func (h *performerASTFilterHandler) handle(ctx context.Context, f *filterBuilder) {
	if h.ast == nil || h.ast.Root == nil {
		return
	}

	predicate, err := compileASTNode(ctx, h.ast.Root, performerASTConditionHandler)
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

func performerASTConditionHandler(condition *models.FilterASTCondition) (criterionHandler, error) {
	qb := &performerFilterHandler{}

	switch condition.Field {
	case "name":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "performers.name"), nil
	case "disambiguation":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "performers.disambiguation"), nil
	case "details":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "performers.details"), nil
	case "ethnicity":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "performers.ethnicity"), nil
	case "country":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "performers.country"), nil
	case "eye_color":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "performers.eye_color"), nil
	case "measurements":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "performers.measurements"), nil
	case "fake_tits":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "performers.fake_tits"), nil
	case "tattoos":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "performers.tattoos"), nil
	case "piercings":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "performers.piercings"), nil
	case "hair_color":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "performers.hair_color"), nil
	case "filter_favorites":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return boolCriterionHandler(&input, "performers.favorite", nil), nil
	case "ignore_auto_tag":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return boolCriterionHandler(&input, "performers.ignore_auto_tag", nil), nil
	case "birth_year":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return yearFilterCriterionHandler(&input, "performers.birthdate"), nil
	case "death_year":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return yearFilterCriterionHandler(&input, "performers.death_date"), nil
	case "age":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performerAgeFilterCriterionHandler(&input), nil
	case "gender":
		input, err := decodeASTValue[models.GenderCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return criterionHandlerFunc(func(ctx context.Context, f *filterBuilder) {
			if input.Value.IsValid() && len(input.ValueList) == 0 {
				input.ValueList = []models.GenderEnum{input.Value}
			}
			v := utils.StringerSliceToStringSlice(input.ValueList)
			enumCriterionHandler(input.Modifier, v, "performers.gender")(ctx, f)
		}), nil
	case "circumcised":
		input, err := decodeASTValue[models.CircumcisionCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return criterionHandlerFunc(func(ctx context.Context, f *filterBuilder) {
			v := utils.StringerSliceToStringSlice(input.Value)
			enumCriterionHandler(input.Modifier, v, "performers.circumcised")(ctx, f)
		}), nil
	case "is_missing":
		input, err := decodeASTValue[string](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performerIsMissingCriterionHandler(&input), nil
	case "height_cm":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return intCriterionHandler(&input, "performers.height", nil), nil
	case "penis_length":
		input, err := decodeASTValue[models.FloatCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return floatCriterionHandler(&input, "performers.penis_length", nil), nil
	case "rating100":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return intCriterionHandler(&input, "performers.rating", nil), nil
	case "url":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.urlsCriterionHandler(&input), nil
	case "weight":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return intCriterionHandler(&input, "performers.weight", nil), nil
	case "stash_id_endpoint":
		input, err := decodeASTValue[models.StashIDCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &stashIDCriterionHandler{
			c:                 &input,
			stashIDRepository: &performerRepository.stashIDs,
			stashIDTableAs:    "performer_stash_ids",
			parentIDCol:       "performers.id",
		}, nil
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
	case "tag_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.tagCountCriterionHandler(&input), nil
	case "studios":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.studiosCriterionHandler(&input), nil
	case "groups":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.groupsCriterionHandler(&input), nil
	case "performers":
		input, err := decodeASTValue[models.MultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.appearsWithCriterionHandler(&input), nil
	case "scene_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.sceneCountCriterionHandler(&input), nil
	case "marker_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.markerCountCriterionHandler(&input), nil
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
	case "play_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.playCounterCriterionHandler(&input), nil
	case "o_counter":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.oCounterCriterionHandler(&input), nil
	case "birthdate":
		input, err := decodeASTValue[models.DateCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &dateCriterionHandler{c: &input, column: "performers.birthdate"}, nil
	case "death_date":
		input, err := decodeASTValue[models.DateCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &dateCriterionHandler{c: &input, column: "performers.death_date"}, nil
	case "career_start":
		input, err := decodeASTValue[models.DateCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &dateCriterionHandler{c: &input, column: "performers.career_start"}, nil
	case "career_end":
		input, err := decodeASTValue[models.DateCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &dateCriterionHandler{c: &input, column: "performers.career_end"}, nil
	case "created_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "performers.created_at"}, nil
	case "updated_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "performers.updated_at"}, nil
	case "custom_fields":
		input, err := decodeASTValue[[]models.CustomFieldCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &customFieldsFilterHandler{
			table: performersCustomFieldsTable.GetTable(),
			fkCol: performerIDColumn,
			c:     input,
			idCol: "performers.id",
		}, nil
	default:
		return nil, fmt.Errorf("performer AST condition %q is not supported yet", condition.Field)
	}
}
