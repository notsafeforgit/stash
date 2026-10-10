package ingest

import (
	"context"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// Legacy images may have post associations without a captured attachment.
// Explicit local review establishes conversion authority from the old file's
// unique owner without manufacturing a source capture or changing those links.
func (s *Service) manualGIFTransformation(ctx context.Context, input ManualFileInput, root models.MediaRoot, sensitive bool) (*IntakeTransformation, error) {
	if err := archive.RequireMediaRootPathAbsent(root, input.OriginalGIFPath); err != nil {
		return nil, err
	}
	original, err := CaptureFileTarget(ctx, s.Repo, root, input.OriginalGIFPath, sensitive)
	if err != nil {
		return nil, err
	}
	if original.FileUUID == "" {
		return nil, ErrManualFileChanged
	}
	owners, err := s.Repo.FileContent.Owners(ctx, original.FileUUID)
	if err != nil {
		return nil, err
	}
	if len(owners) != 1 || owners[0].Kind != models.ArchiveImage || owners[0].State != models.ArchiveEntityActive || owners[0].LocalID == nil {
		return nil, ErrManualFileChanged
	}
	image, err := s.Repo.Image.Find(ctx, *owners[0].LocalID)
	if err != nil {
		return nil, err
	}
	if image == nil {
		return nil, ErrManualFileChanged
	}
	if err := image.LoadFiles(ctx, s.Repo.Image); err != nil {
		return nil, err
	}
	if len(image.Files.List()) != 1 || image.Files.List()[0].Base().ZipFileID != nil {
		return nil, ErrManualFileChanged
	}
	result := &IntakeTransformation{Kind: "gif-to-video", Original: *original,
		ReviewedImageUUID: owners[0].UUID, ReviewedImageRevision: owners[0].Revision}
	target, err := CaptureFileTarget(ctx, s.Repo, root, input.RelativePath, sensitive)
	if err != nil {
		return nil, err
	}
	if target.FileUUID != "" {
		owners, err = s.Repo.FileContent.Owners(ctx, target.FileUUID)
		if err != nil {
			return nil, err
		}
		if len(owners) > 1 || (len(owners) == 1 && (owners[0].Kind != models.ArchiveScene || owners[0].State != models.ArchiveEntityActive)) {
			return nil, ErrManualFileChanged
		}
		if len(owners) == 1 {
			result.ReviewedSceneUUID, result.ReviewedSceneRevision = owners[0].UUID, owners[0].Revision
		}
	}
	return result, nil
}
