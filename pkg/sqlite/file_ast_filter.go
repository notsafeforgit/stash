package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

type fileASTFilterHandler struct {
	ast     *models.FilterAST
	related bool
}
type folderASTFilterHandler struct {
	ast     *models.FilterAST
	related bool
}

func applyStorageAST(ctx context.Context, f *filterBuilder, ast *models.FilterAST, condition conditionHandlerFn) {
	if ast == nil {
		return
	}
	if err := ast.Validate(); err != nil {
		f.setError(err)
		return
	}
	predicate, err := compileASTNode(ctx, ast.Root, condition)
	if err != nil {
		f.setError(err)
		return
	}
	*f = *predicate.toFilterBuilder()
}

func (h *fileASTFilterHandler) handle(ctx context.Context, f *filterBuilder) {
	applyStorageAST(ctx, f, h.ast, func(c *models.FilterASTCondition) (criterionHandler, error) {
		handler, err := fileASTCondition(c, h.related)
		if err != nil {
			return nil, err
		}
		return storageASTLeaf(fileRepository.repository, handler, true), nil
	})
}
func (h *folderASTFilterHandler) handle(ctx context.Context, f *filterBuilder) {
	applyStorageAST(ctx, f, h.ast, func(c *models.FilterASTCondition) (criterionHandler, error) {
		handler, err := folderASTCondition(c, h.related)
		if err != nil {
			return nil, err
		}
		return storageASTLeaf(folderRepository.repository, handler, false), nil
	})
}

// Isolate each condition's joins and aggregates before composing AND/OR.
// Otherwise a count predicate in HAVING can incorrectly narrow an OR branch.
func storageASTLeaf(repo repository, handler criterionHandler, file bool) criterionHandler {
	return criterionHandlerFunc(func(ctx context.Context, f *filterBuilder) {
		built := filterBuilderFromHandler(ctx, handler)
		query := repo.newQuery()
		selectIDs(&query, repo.tableName)
		if file {
			query.join(folderTable, "", "files.parent_folder_id = folders.id")
		}
		if err := query.addFilter(built); err != nil {
			f.setError(err)
			return
		}
		f.whereClauses = append(f.whereClauses, makeClause(fmt.Sprintf("%s.id IN (%s)", repo.tableName, query.toSQL(false)), query.allArgs()...))
	})
}

func fileASTCondition(c *models.FilterASTCondition, related bool) (criterionHandler, error) {
	if c.Value == nil {
		return nil, fmt.Errorf("file AST condition %q requires a value", c.Field)
	}
	switch c.Field {
	case "path", "basename", "dir", "parent_folder", "zip_file", "mod_time", "duplicated", "hashes", "scene_count", "image_count", "gallery_count", "created_at", "updated_at":
		input, err := decodeASTValue[models.FileFilterType](map[string]any{c.Field: c.Value})
		if err != nil {
			return nil, err
		}
		return (&fileFilterHandler{fileFilter: &input}).criterionHandler(), nil
	case "scenes", "images", "galleries":
		if related {
			return nil, fmt.Errorf("nested related file AST conditions are not supported")
		}
		ast, err := decodeRelatedStorageAST(c.Value)
		if err != nil {
			return nil, err
		}
		switch c.Field {
		case "scenes":
			return &relatedFilterHandler{relatedIDCol: "scenes_files.scene_id", relatedRepo: sceneRepository.repository, relatedHandler: &sceneASTFilterHandler{ast: ast}, joinFn: func(f *filterBuilder) { fileRepository.scenes.innerJoin(f, "", "files.id") }}, nil
		case "images":
			return &relatedFilterHandler{relatedIDCol: "images_files.image_id", relatedRepo: imageRepository.repository, relatedHandler: &imageASTFilterHandler{ast: ast}, joinFn: func(f *filterBuilder) { fileRepository.images.innerJoin(f, "", "files.id") }}, nil
		default:
			return &relatedFilterHandler{relatedIDCol: "galleries_files.gallery_id", relatedRepo: galleryRepository.repository, relatedHandler: &galleryASTFilterHandler{ast: ast}, joinFn: func(f *filterBuilder) { fileRepository.galleries.innerJoin(f, "", "files.id") }}, nil
		}
	case "video.resolution", "video.orientation", "video.framerate", "video.bitrate", "video.bit_depth", "video.video_stream_duration", "video.frame_count", "video.duration_mismatch", "video.format", "video.video_codec", "video.audio_codec", "video.duration", "video.captions", "video.interactive", "video.interactive_speed":
		input, err := decodeASTValue[models.VideoFileFilterInput](map[string]any{strings.TrimPrefix(c.Field, "video."): c.Value})
		if err != nil {
			return nil, err
		}
		return &videoFileFilterHandler{filter: &input}, nil
	case "image.format", "image.resolution", "image.bit_depth", "image.orientation":
		input, err := decodeASTValue[models.ImageFileFilterInput](map[string]any{strings.TrimPrefix(c.Field, "image."): c.Value})
		if err != nil {
			return nil, err
		}
		return &imageFileFilterHandler{filter: &input}, nil
	default:
		return nil, fmt.Errorf("file AST condition %q is not supported", c.Field)
	}
}

func folderASTCondition(c *models.FilterASTCondition, related bool) (criterionHandler, error) {
	if c.Value == nil {
		return nil, fmt.Errorf("folder AST condition %q requires a value", c.Field)
	}
	switch c.Field {
	case "path", "basename", "parent_folder", "zip_file", "mod_time", "gallery_count", "created_at", "updated_at":
		input, err := decodeASTValue[models.FolderFilterType](map[string]any{c.Field: c.Value})
		if err != nil {
			return nil, err
		}
		return (&folderFilterHandler{folderFilter: &input}).criterionHandler(), nil
	case "files", "galleries":
		if related {
			return nil, fmt.Errorf("nested related folder AST conditions are not supported")
		}
		ast, err := decodeRelatedStorageAST(c.Value)
		if err != nil {
			return nil, err
		}
		if c.Field == "files" {
			return &relatedFilterHandler{relatedIDCol: "files.id", relatedRepo: fileRepository.repository, relatedHandler: &fileASTFilterHandler{ast: ast, related: true}, joinFn: func(f *filterBuilder) { f.addInnerJoin("files", "", "files.parent_folder_id = folders.id") }}, nil
		}
		return &relatedFilterHandler{relatedIDCol: "galleries.id", relatedRepo: galleryRepository.repository, relatedHandler: &galleryASTFilterHandler{ast: ast}, joinFn: func(f *filterBuilder) { folderRepository.galleries.innerJoin(f, "", "folders.id") }}, nil
	default:
		return nil, fmt.Errorf("folder AST condition %q is not supported", c.Field)
	}
}

func decodeRelatedStorageAST(value any) (*models.FilterAST, error) {
	ast, err := decodeASTValue[models.FilterAST](value)
	if err != nil {
		return nil, err
	}
	if err := ast.Validate(); err != nil {
		return nil, err
	}
	return &ast, nil
}
