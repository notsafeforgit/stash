package ingest

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// FileTransformation records the configured postprocessor's original path.
// It is a transformation claim, not proof that the two files have equal bytes
// or authority to replace a library entity. The final bytes are verified by
// the file worker; any identity transition must also validate the original
// file lifetime and the current attachment's selected media.
type FileTransformation struct {
	Kind                 string `json:"kind"`
	OriginalRelativePath string `json:"original_relative_path"`
}

// IntakeTransformation freezes the original file's server-owned lifetime at
// admission. A producer cannot supply an image UUID or bypass a removed path.
type IntakeTransformation struct {
	Kind     string     `json:"kind"`
	Original FileTarget `json:"original"`
}

// Retain the portable claim with source-media evidence after publication.
// Admission's host path and internal removal fence remain private job state.
type fileTransformationEvidence struct {
	FileTransformation
	OriginalFileUUID   string `json:"original_file_uuid,omitempty"`
	OriginalGeneration int64  `json:"original_generation,omitempty"`
}

func transformationEvidence(value *IntakeTransformation) *fileTransformationEvidence {
	if value == nil {
		return nil
	}
	return &fileTransformationEvidence{
		FileTransformation: FileTransformation{Kind: value.Kind, OriginalRelativePath: value.Original.RelativePath},
		OriginalFileUUID:   value.Original.FileUUID, OriginalGeneration: value.Original.Generation,
	}
}

func validFileTransformation(value *FileTransformation, kind models.ArchiveEntityKind, final string, sourced bool) bool {
	if value == nil {
		return true
	}
	original := value.OriginalRelativePath
	extension := path.Ext(original)
	return value.Kind == "gif-to-video" && sourced && kind == models.ArchiveScene &&
		archive.ValidRootRelativePath(original, false) && strings.EqualFold(extension, ".gif") &&
		strings.TrimSuffix(original, extension)+".mkv" == final
}

func validIntakeTransformation(input IntakePublication) bool {
	value := input.Transformation
	if value == nil {
		return true
	}
	original := value.Original
	if !validFileTransformation(&FileTransformation{Kind: value.Kind, OriginalRelativePath: original.RelativePath}, input.Kind, input.Target.RelativePath, input.Source != nil) ||
		original.RootUUID != input.Target.RootUUID || !ValidUUID(original.RootUUID) ||
		original.PathFence.CaseSensitive != input.Target.PathFence.CaseSensitive ||
		filepath.Dir(original.PathFence.Path) != filepath.Dir(input.Target.PathFence.Path) ||
		filepath.Base(original.PathFence.Path) != path.Base(original.RelativePath) {
		return false
	}
	return (original.FileUUID == "" && original.Generation == 0) || (ValidUUID(original.FileUUID) && original.Generation > 0)
}
