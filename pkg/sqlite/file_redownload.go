package sqlite

import (
	"context"
	"path/filepath"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// RedownloadOwner accounts for the complete removal fence using immutable
// deduplication receipts. A receipt alone cannot excuse a later user deletion
// or move: those advance the fence without another deduplication receipt.
// This reads existing native evidence; it neither rewrites old receipts nor
// resurrects a removed file identity.
func (s *FileDeduplicationStore) RedownloadOwner(ctx context.Context, input models.FileRedownload) (*models.ArchiveEntity, error) {
	if id, err := archiveUUID(input.RootUUID); err != nil || id != input.RootUUID || !archive.ValidRootRelativePath(input.RelativePath, false) ||
		!archive.ValidSHA256(input.SHA256) || input.Size < 1 || input.Fence.Revision < 1 || input.Fence.Revision > 1000 {
		return nil, models.ErrFilePathChanged
	}
	if err := (&FilePathStore{}).Check(ctx, input.Fence); err != nil {
		return nil, err
	}
	root, err := (&MediaRootStore{}).Find(ctx, input.RootUUID)
	if err != nil {
		return nil, err
	}
	if root == nil || root.State != "active" || root.Binding == nil || filepath.Join(root.Binding.Path, filepath.FromSlash(input.RelativePath)) != input.Fence.Path {
		return nil, models.ErrFilePathChanged
	}
	content, err := (&FileContentStore{}).FindBySHA256(ctx, input.SHA256)
	if err != nil {
		return nil, err
	}
	if content == nil || content.Size != input.Size {
		return nil, models.ErrFilePathChanged
	}
	// Start at one content's indexed immutable versions and the unique removed
	// file/generation key. Do not scan the library or load large saved proofs.
	// The signed root binding also accounts for the absolute local path fence;
	// a receipt from a former mount cannot excuse a removal at the new mount.
	var rows []struct {
		Kept    string `db:"kept_file_uuid"`
		Removed string `db:"removed_file_uuid"`
		Media   string `db:"media_uuid"`
	}
	if err := dbWrapper.Select(ctx, &rows, redownloadReceiptsQuery, content.UUID, input.RootUUID, input.RelativePath, root.Binding.Path); err != nil {
		return nil, err
	}
	if int64(len(rows)) != input.Fence.Revision {
		return nil, models.ErrFilePathChanged
	}
	var owner *models.ArchiveEntity
	identities, contents := &ArchiveEntityStore{}, &FileContentStore{}
	for _, row := range rows {
		media, err := identities.Resolve(ctx, row.Media)
		if err != nil {
			return nil, err
		}
		if media == nil || media.State != models.ArchiveEntityActive || (media.Kind != models.ArchiveScene && media.Kind != models.ArchiveImage) ||
			(owner != nil && owner.UUID != media.UUID) {
			return nil, models.ErrArchiveIdentityConflict
		}
		kept, err := identities.Resolve(ctx, row.Kept)
		if err != nil {
			return nil, err
		}
		removed, err := identities.Resolve(ctx, row.Removed)
		if err != nil {
			return nil, err
		}
		if kept == nil || kept.State != models.ArchiveEntityActive || kept.Kind != models.ArchiveFile || removed == nil || removed.UUID != kept.UUID {
			return nil, models.ErrFileGenerationConflict
		}
		proof, err := contents.Current(ctx, kept.UUID)
		if err != nil {
			return nil, err
		}
		if proof == nil || proof.Content.UUID != content.UUID {
			return nil, models.ErrFileContentConflict
		}
		owners, err := contents.Owners(ctx, kept.UUID)
		if err != nil {
			return nil, err
		}
		if len(owners) != 1 || owners[0].UUID != media.UUID {
			return nil, models.ErrArchiveIdentityConflict
		}
		owner = media
	}
	return owner, nil
}

const redownloadReceiptsQuery = `SELECT d.kept_file_uuid,d.removed_file_uuid,d.media_uuid
FROM file_content_versions v INDEXED BY file_content_versions_content
CROSS JOIN file_deduplications d ON d.removed_file_uuid=v.file_uuid AND d.removed_generation=v.generation
WHERE v.content_uuid=? AND d.root_uuid=? AND d.remove_path=?
AND json_extract(d.proof,'$.state.root.binding.path')=? LIMIT 1001`
