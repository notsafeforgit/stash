package sqlite

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

type imageASTFilterHandler struct {
	ast *models.FilterAST
}

func (h *imageASTFilterHandler) handle(ctx context.Context, f *filterBuilder) {
	if h.ast == nil || h.ast.Root == nil {
		return
	}

	predicate, err := compileASTNode(ctx, h.ast.Root, imageASTConditionHandler)
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

func imageASTConditionHandler(condition *models.FilterASTCondition) (criterionHandler, error) {
	qb := &imageFilterHandler{}

	switch condition.Field {
	case "title":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "images.title"), nil
	case "code":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "images.code"), nil
	case "details":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "images.details"), nil
	case "photographer":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "images.photographer"), nil
	case "checksum":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return criterionHandlerFunc(func(ctx context.Context, f *filterBuilder) {
			imageRepository.addImagesFilesTable(f, joinTypeLeft)
			f.addInnerJoin(fingerprintTable, "fingerprints_md5", "images_files.file_id = fingerprints_md5.file_id AND fingerprints_md5.type = 'md5'")
			stringCriterionHandler(&input, "fingerprints_md5.fingerprint")(ctx, f)
		}), nil
	case "phash_distance":
		input, err := decodeASTValue[models.PhashDistanceCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &phashDistanceCriterionHandler{
			joinFn: func(f *filterBuilder) {
				imageRepository.addImagesFilesTable(f, joinTypeLeft)
				f.addLeftJoin(fingerprintTable, "fingerprints_phash", "images_files.file_id = fingerprints_phash.file_id AND fingerprints_phash.type = 'phash'")
			},
			criterion: &input,
		}, nil
	case "path":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return pathCriterionHandler(&input, "folders.path", "files.basename", imageRepository.addFoldersTable), nil
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
		return intCriterionHandler(&input, "images.rating", nil), nil
	case "o_counter":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return intCriterionHandler(&input, "images.o_counter", nil), nil
	case "organized":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return boolCriterionHandler(&input, "images.organized", nil), nil
	case "date":
		input, err := decodeASTValue[models.DateCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &dateCriterionHandler{c: &input, column: "images.date"}, nil
	case "url":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.urlsCriterionHandler(&input), nil
	case "resolution":
		input, err := decodeASTValue[models.ResolutionCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return resolutionCriterionHandler(&input, "image_files.height", "image_files.width", imageRepository.addImageFilesTable), nil
	case "bit_depth":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return intCriterionHandler(&input, "fork_image_file_metadata.bit_depth", imageRepository.addImageFileMetadataTable), nil
	case "orientation":
		input, err := decodeASTValue[models.OrientationCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return orientationCriterionHandler(&input, "image_files.height", "image_files.width", imageRepository.addImageFilesTable), nil
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
	case "galleries":
		input, err := decodeASTValue[models.MultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.galleriesCriterionHandler(&input), nil
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
	case "studios":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return studioCriterionHandler(imageTable, &input), nil
	case "performer_tags":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performerTagsCriterionHandler(&input), nil
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
	case "created_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "images.created_at"}, nil
	case "updated_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "images.updated_at"}, nil
	case "custom_fields":
		input, err := decodeASTValue[[]models.CustomFieldCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &customFieldsFilterHandler{
			table: imagesCustomFieldsTable.GetTable(),
			fkCol: imageIDColumn,
			c:     input,
			idCol: "images.id",
		}, nil
	default:
		return nil, fmt.Errorf("image AST condition %q is not supported yet", condition.Field)
	}
}
