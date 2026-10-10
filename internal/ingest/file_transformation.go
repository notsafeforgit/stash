package ingest

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
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

func (p *PreparedMedia) publishIntakeMedia(ctx context.Context, repo models.Repository, input IntakePublication, selected *models.ArchiveEntity) (*PublishedMedia, *IntakeConversionResult, error) {
	if selected == nil && input.Transformation != nil && input.Transformation.Original.FileUUID != "" {
		// An explicit source unlink must survive conversion. The original file's
		// unique image owner and retained attachment/file evidence can establish
		// the conversion without restoring that source association.
		owners, err := repo.FileContent.Owners(ctx, input.Transformation.Original.FileUUID)
		if err != nil {
			return nil, nil, err
		}
		if len(owners) > 1 {
			return nil, nil, ErrAmbiguousMedia
		}
		if len(owners) == 1 && owners[0].Kind == models.ArchiveImage {
			for after, checked := "", 0; selected == nil && checked < 1000; {
				evidence, err := repo.SourceAttachment.MediaEvidence(ctx, input.Source.AttachmentUUID, after, 100)
				if err != nil {
					return nil, nil, err
				}
				for _, item := range evidence {
					after, checked = item.UUID, checked+1
					if item.FileUUID == nil || *item.FileUUID != input.Transformation.Original.FileUUID {
						continue
					}
					candidate, err := repo.ArchiveEntity.Resolve(ctx, item.MediaUUID)
					if err != nil {
						return nil, nil, err
					}
					if candidate != nil && candidate.UUID == owners[0].UUID {
						selected = owners[0]
					}
				}
				if len(evidence) < 100 {
					break
				}
			}
			if selected == nil {
				return nil, nil, ErrMediaKindConflict
			}
		}
	}
	if selected == nil || selected.Kind == input.Kind || input.Kind != models.ArchiveScene || selected.Kind != models.ArchiveImage || input.Transformation == nil {
		media, err := p.publishMedia(ctx, repo, input.Target, input.Kind, selected)
		return media, nil, err
	}
	original := input.Transformation.Original
	if original.FileUUID == "" || selected.LocalID == nil {
		return nil, nil, ErrMediaKindConflict
	}
	if err := repo.FilePath.Check(ctx, original.PathFence); err != nil {
		return nil, nil, err
	}
	current, err := repo.File.FindByPath(ctx, original.PathFence.Path, original.PathFence.CaseSensitive)
	if err != nil {
		return nil, nil, err
	}
	identity, err := repo.ArchiveEntity.Find(ctx, original.FileUUID)
	if err != nil {
		return nil, nil, err
	}
	if current == nil || identity == nil || identity.State != models.ArchiveEntityActive || identity.LocalID == nil ||
		models.FileID(*identity.LocalID) != current.Base().ID || current.Base().Generation != original.Generation ||
		current.Base().ZipFileID != nil {
		return nil, nil, models.ErrFileGenerationConflict
	}
	// GIFs may have been probed as video files while still belonging to an
	// image entity. The configured same-stem .gif path and selected image owner
	// establish the original side; its scanner subtype is not the media kind.
	image, err := repo.Image.Find(ctx, *selected.LocalID)
	if err != nil {
		return nil, nil, err
	}
	if image == nil {
		return nil, nil, models.ErrArchiveIdentityConflict
	}
	absent := func(ctx context.Context) error {
		root, err := repo.MediaRoot.Find(ctx, original.RootUUID)
		if err != nil {
			return err
		}
		if root == nil || root.Binding == nil || filepath.Join(root.Binding.Path, filepath.FromSlash(original.RelativePath)) != original.PathFence.Path {
			return ErrDefinition
		}
		return archive.RequireMediaRootPathAbsent(*root, original.RelativePath)
	}
	if err := absent(ctx); err != nil {
		return nil, nil, fmt.Errorf("verify converted GIF: %w", err)
	}
	media, err := p.publishMedia(ctx, repo, input.Target, input.Kind, nil)
	if err != nil {
		return nil, nil, err
	}
	if !media.Created {
		return nil, nil, ErrAmbiguousMedia
	}
	conversionUUID := uuid.NewSHA1(uuid.MustParse(input.UUID), []byte("gif-to-video")).String()
	converted, err := repo.MediaConversion.ConvertImage(ctx, models.ImageConversionInput{
		UUID: conversionUUID, ImageUUID: selected.UUID, ExpectedImageRevision: selected.Revision,
		SceneUUID: media.Media.UUID, ExpectedSceneRevision: media.Media.Revision,
		OriginalFileUUID: original.FileUUID, OriginalGeneration: original.Generation,
		FileUUID: media.File.Identity.UUID, Generation: media.File.File.Base().Generation,
	})
	if err != nil {
		return nil, nil, err
	}
	ret := &IntakeConversionResult{UUID: converted.UUID, ImageUUID: selected.UUID, ImageID: *selected.LocalID, FileUUID: original.FileUUID, GalleryUUIDs: []string{}}
	ret.FileID, ret.Checksum = int(current.Base().ID), image.Checksum
	ret.Fingerprints = make(map[string]string)
	for _, fingerprint := range current.Base().Fingerprints {
		ret.Fingerprints[fingerprint.Type] = fingerprint.Value()
	}
	galleries, err := repo.Scene.GetGalleryIDs(ctx, *media.Media.LocalID)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range galleries {
		gallery, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveGallery, id)
		if err != nil {
			return nil, nil, err
		}
		if gallery == nil {
			return nil, nil, models.ErrArchiveIdentityConflict
		}
		ret.GalleryUUIDs = append(ret.GalleryUUIDs, gallery.UUID)
	}
	// The expected removal was just committed to this transaction. Any further
	// replacement/removal of the old path before commit still invalidates it.
	fence, err := repo.FilePath.Snapshot(ctx, original.PathFence.Path, original.PathFence.CaseSensitive)
	if err != nil {
		return nil, nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		if err := repo.FilePath.Check(ctx, *fence); err != nil {
			return err
		}
		return absent(ctx)
	})
	return media, ret, nil
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
