package sqlite

import (
	"context"
	"fmt"
	"reflect"

	"github.com/stashapp/stash/pkg/models"
)

type sceneASTFilterHandler struct {
	ast *models.FilterAST
}

func (h *sceneASTFilterHandler) handle(ctx context.Context, f *filterBuilder) {
	if h.ast == nil || h.ast.Root == nil {
		return
	}

	predicate, err := compileASTNode(ctx, h.ast.Root, sceneASTConditionHandler)
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

func sceneASTConditionHandler(condition *models.FilterASTCondition) (criterionHandler, error) {
	qb := &sceneFilterHandler{}

	switch condition.Field {
	case "title":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "scenes.title"), nil
	case "code":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "scenes.code"), nil
	case "path":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return pathCriterionHandler(&input, "folders.path", "files.basename", qb.addFoldersTable), nil
	case "folder":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return criterionHandlerFunc(func(ctx context.Context, f *filterBuilder) {
			qb.addFilesTable(f, joinTypeLeft)
			qb.addFoldersTable(f, joinTypeLeft)
			(&fileFilterHandler{}).parentFolderCriterionHandler(&input)(ctx, f)
		}), nil
	case "details":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "scenes.details"), nil
	case "director":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return stringCriterionHandler(&input, "scenes.director"), nil
	case "oshash":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return criterionHandlerFunc(func(ctx context.Context, f *filterBuilder) {
			qb.addSceneFilesTable(f, joinTypeLeft)
			f.addLeftJoin(fingerprintTable, "fingerprints_oshash", "scenes_files.file_id = fingerprints_oshash.file_id AND fingerprints_oshash.type = 'oshash'")
			stringCriterionHandler(&input, "fingerprints_oshash.fingerprint")(ctx, f)
		}), nil
	case "checksum":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return criterionHandlerFunc(func(ctx context.Context, f *filterBuilder) {
			qb.addSceneFilesTable(f, joinTypeLeft)
			f.addLeftJoin(fingerprintTable, "fingerprints_md5", "scenes_files.file_id = fingerprints_md5.file_id AND fingerprints_md5.type = 'md5'")
			stringCriterionHandler(&input, "fingerprints_md5.fingerprint")(ctx, f)
		}), nil
	case "phash_distance":
		input, err := decodeASTValue[models.PhashDistanceCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &phashDistanceCriterionHandler{
			joinFn: func(f *filterBuilder) {
				qb.addSceneFilesTable(f, joinTypeLeft)
				f.addLeftJoin(fingerprintTable, "fingerprints_phash", "scenes_files.file_id = fingerprints_phash.file_id AND fingerprints_phash.type = 'phash'")
			},
			criterion: &input,
		}, nil
	case "duplicated":
		input, err := decodeASTValue[models.DuplicationCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.duplicatedCriterionHandler(&input), nil
	case "date":
		input, err := decodeASTValue[models.DateCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &dateCriterionHandler{c: &input, column: "scenes.date"}, nil
	case "created_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "scenes.created_at"}, nil
	case "updated_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &timestampCriterionHandler{c: &input, column: "scenes.updated_at"}, nil
	case "last_played_at":
		input, err := decodeASTValue[models.TimestampCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return criterionHandlerFunc(func(ctx context.Context, f *filterBuilder) {
			f.addLeftJoin(
				fmt.Sprintf("(SELECT %s, MAX(%s) as last_played_at FROM %s GROUP BY %s)", sceneIDColumn, sceneViewDateColumn, scenesViewDatesTable, sceneIDColumn),
				"scene_last_view",
				fmt.Sprintf("scene_last_view.%s = scenes.id", sceneIDColumn),
			)
			h := timestampCriterionHandler{c: &input, column: "IFNULL(last_played_at, datetime(0))"}
			h.handle(ctx, f)
		}), nil
	case "rating100":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return intCriterionHandler(&input, "scenes.rating", nil), nil
	case "o_counter":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.oCountCriterionHandler(&input), nil
	case "organized":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return boolCriterionHandler(&input, "scenes.organized", nil), nil
	case "resolution":
		input, err := decodeASTValue[models.ResolutionCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return resolutionCriterionHandler(&input, "video_files.height", "video_files.width", qb.addVideoFilesTable), nil
	case "orientation":
		input, err := decodeASTValue[models.OrientationCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return orientationCriterionHandler(&input, "video_files.height", "video_files.width", qb.addVideoFilesTable), nil
	case "framerate":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return floatIntCriterionHandler(&input, "ROUND(video_files.frame_rate)", qb.addVideoFilesTable), nil
	case "bitrate":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return intCriterionHandler(&input, "video_files.bit_rate", qb.addVideoFilesTable), nil
	case "bit_depth":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return intCriterionHandler(&input, "video_files.bit_depth", qb.addVideoFilesTable), nil
	case "video_stream_duration":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return floatIntCriterionHandler(&input, "video_files.video_stream_duration", qb.addVideoFilesTable), nil
	case "frame_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return intCriterionHandler(&input, "video_files.frame_count", qb.addVideoFilesTable), nil
	case "duration_mismatch":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return durationMismatchCriterionHandler(&input, qb.addVideoFilesTable), nil
	case "video_codec":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.codecCriterionHandler(&input, "video_files.video_codec", qb.addVideoFilesTable), nil
	case "audio_codec":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.codecCriterionHandler(&input, "video_files.audio_codec", qb.addVideoFilesTable), nil
	case "duration":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return floatIntCriterionHandler(&input, "video_files.duration", qb.addVideoFilesTable), nil
	case "resume_time":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return floatIntCriterionHandler(&input, "scenes.resume_time", nil), nil
	case "play_duration":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return floatIntCriterionHandler(&input, "scenes.play_duration", nil), nil
	case "play_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.playCountCriterionHandler(&input), nil
	case "has_markers":
		input, err := decodeASTValue[string](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.hasMarkersCriterionHandler(&input), nil
	case "is_missing":
		input, err := decodeASTValue[string](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.isMissingCriterionHandler(&input), nil
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
	case "performer_tags":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performerTagsCriterionHandler(&input), nil
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
	case "performer_age":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performerAgeCriterionHandler(&input), nil
	case "performer_favorite":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.performerFavoriteCriterionHandler(&input), nil
	case "studios":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return studioCriterionHandler(sceneTable, &input), nil
	case "groups":
		input, err := decodeASTValue[models.HierarchicalMultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.groupsCriterionHandler(&input), nil
	case "galleries":
		input, err := decodeASTValue[models.MultiCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.galleriesCriterionHandler(&input), nil
	case "url":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.urlsCriterionHandler(&input), nil
	case "stash_id_endpoint":
		input, err := decodeASTValue[models.StashIDCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &stashIDCriterionHandler{
			c:                 &input,
			stashIDRepository: &sceneRepository.stashIDs,
			stashIDTableAs:    "scene_stash_ids",
			parentIDCol:       "scenes.id",
		}, nil
	case "stash_id_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.stashIDCountCriterionHandler(&input), nil
	case "interactive":
		input, err := decodeASTValue[bool](condition.Value)
		if err != nil {
			return nil, err
		}
		return boolCriterionHandler(&input, "video_files.interactive", qb.addVideoFilesTable), nil
	case "interactive_speed":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return intCriterionHandler(&input, "video_files.interactive_speed", qb.addVideoFilesTable), nil
	case "captions":
		input, err := decodeASTValue[models.StringCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.captionCriterionHandler(&input), nil
	case "studio_tags":
		input, err := decodeASTValue[models.StudioTagFilterInput](condition.Value)
		if err != nil {
			return nil, err
		}
		studioFilter, err := sceneStudioTagFilterToStudioFilter(input)
		if err != nil {
			return nil, err
		}
		return &relatedFilterHandler{
			relatedIDCol:          "scenes.studio_id",
			relatedRepo:           studioRepository.repository,
			relatedHandler:        &studioFilterHandler{studioFilter},
			includeMissingRelated: relatedFilterIncludesMissingRelation(studioFilter),
		}, nil
	case "file_count":
		input, err := decodeASTValue[models.IntCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return qb.fileCountCriterionHandler(&input), nil
	case "custom_fields":
		input, err := decodeASTValue[[]models.CustomFieldCriterionInput](condition.Value)
		if err != nil {
			return nil, err
		}
		return &customFieldsFilterHandler{
			table: scenesCustomFieldsTable.GetTable(),
			fkCol: sceneIDColumn,
			c:     input,
			idCol: "scenes.id",
		}, nil
	default:
		return nil, fmt.Errorf("scene AST condition %q is not supported yet", condition.Field)
	}
}

func sceneStudioTagFilterToStudioFilter(input models.StudioTagFilterInput) (*models.StudioFilterType, error) {
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
	if active && excludeOnly {
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
