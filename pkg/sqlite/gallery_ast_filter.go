package sqlite

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

type galleryASTFilterHandler struct {
	ast *models.FilterAST
}

func (h *galleryASTFilterHandler) handle(ctx context.Context, f *filterBuilder) {
	if h.ast == nil || h.ast.Root == nil {
		return
	}

	predicate, err := compileASTNode(ctx, h.ast.Root, galleryASTConditionHandler)
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

func galleryASTConditionHandler(condition *models.FilterASTCondition) (criterionHandler, error) {
	qb := &galleryFilterHandler{}

	switch condition.Field {
	case "title":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "galleries.title"), nil
	case "code":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "galleries.code"), nil
	case "details":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "galleries.details"), nil
	case "photographer":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "galleries.photographer"), nil
	case "checksum":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return criterionHandlerFunc(func(ctx context.Context, f *filterBuilder) {
			galleryRepository.addGalleriesFilesTable(f)
			f.addLeftJoin(fingerprintTable, "fingerprints_md5", "galleries_files.file_id = fingerprints_md5.file_id AND fingerprints_md5.type = 'md5'")
			stringCriterionHandler(&input, "fingerprints_md5.fingerprint")(ctx, f)
		}), nil
	case "path":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.pathCriterionHandler(&input), nil
	case "parent_folder":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.parentFolderCriterionHandler(&input), nil
	case "file_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.fileCountCriterionHandler(&input), nil
	case "rating100":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return intCriterionHandler(&input, "galleries.rating", nil), nil
	case "url":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.urlsCriterionHandler(&input), nil
	case "organized":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return boolCriterionHandler(&input, "galleries.organized", nil), nil
	case "is_missing":
		input, err := decodeASTValue[string](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.missingCriterionHandler(&input), nil
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
	case "performers":
		input, err := decodeASTValue[models.MultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performersCriterionHandler(&input), nil
	case "performer_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performerCountCriterionHandler(&input), nil
	case "scenes":
		input, err := decodeASTValue[models.MultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.scenesCriterionHandler(&input), nil
	case "has_chapters":
		input, err := decodeASTValue[string](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.hasChaptersCriterionHandler(&input), nil
	case "studios":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return studioCriterionHandler(galleryTable, &input), nil
	case "performer_tags":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performerTagsCriterionHandler(&input), nil
	case "average_resolution":
		input, err := decodeASTValue[models.ResolutionCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.averageResolutionCriterionHandler(&input), nil
	case "image_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.imageCountCriterionHandler(&input), nil
	case "performer_favorite":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performerFavoriteCriterionHandler(&input), nil
	case "performer_age":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performerAgeCriterionHandler(&input), nil
	case "date":
		input, err := decodeASTValue[models.DateCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &dateCriterionHandler{c: &input, column: "galleries.date"}, nil
	case "created_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "galleries.created_at"}, nil
	case "updated_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "galleries.updated_at"}, nil
	case "custom_fields":
		input, err := decodeASTValue[[]models.CustomFieldCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &customFieldsFilterHandler{
			table: galleriesCustomFieldsTable.GetTable(),
			fkCol: galleryIDColumn,
			c:     input,
			idCol: "galleries.id",
		}, nil
	default:
		return nil, fmt.Errorf("gallery AST condition %q is not supported yet", condition.Field)
	}
}
