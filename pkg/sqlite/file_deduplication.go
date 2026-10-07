package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type FileDeduplicationStore struct{}

const fileDeduplicationSelect = `SELECT uuid,signature,root_uuid,keep_path,remove_path,
 kept_file_uuid,kept_generation,removed_file_uuid,removed_generation,media_uuid,sha256,
 CAST(proof AS BLOB) AS proof,committed_at FROM file_deduplications`

func (s *FileDeduplicationStore) Inspect(ctx context.Context, input models.FileDeduplicationInput) (*models.FileDeduplicationState, error) {
	if id, err := archiveUUID(input.RootUUID); err != nil || id != input.RootUUID ||
		!archive.ValidRootRelativePath(input.KeepPath, false) || !archive.ValidRootRelativePath(input.RemovePath, false) ||
		input.KeepPath == input.RemovePath || strings.HasSuffix(strings.ToLower(input.KeepPath), ".part") || strings.HasSuffix(strings.ToLower(input.RemovePath), ".part") {
		return nil, models.ErrFileDeduplicationInvalid
	}
	root, err := (&MediaRootStore{}).Find(ctx, input.RootUUID)
	if err != nil {
		return nil, err
	}
	if root == nil || root.State != "active" || root.Binding == nil {
		return nil, models.ErrFileDeduplicationConflict
	}
	ret := &models.FileDeduplicationState{Input: input, Root: *root, Matches: []models.SourceFileMatch{}}
	var keptOwner, removedOwner *models.ArchiveEntity
	for i, relative := range []string{input.KeepPath, input.RemovePath} {
		f, owner, reason, err := deduplicationFile(ctx, filepath.Join(root.Binding.Path, filepath.FromSlash(relative)))
		if err != nil {
			return nil, err
		}
		if reason != "" {
			ret.BlockedReason = reason
			return ret, nil
		}
		if i == 0 {
			ret.Kept, keptOwner = f, owner
		} else {
			ret.Removed, removedOwner = f, owner
		}
	}
	if ret.Kept.UUID == ret.Removed.UUID || ret.Kept.Size == 0 || ret.Kept.Size != ret.Removed.Size {
		ret.BlockedReason = "different_or_empty_files"
		return ret, nil
	}
	if keptOwner.UUID != removedOwner.UUID {
		ret.BlockedReason = "different_media_owners"
		return ret, nil
	}
	ret.Owner = keptOwner
	// Caption sidecars are not interchangeable just because the video bytes are.
	var captions bool
	if err := dbWrapper.Get(ctx, &captions, "SELECT EXISTS(SELECT 1 FROM video_captions WHERE file_id=?)", ret.Removed.ID); err != nil {
		return nil, err
	}
	if captions {
		ret.BlockedReason = "duplicate_has_captions"
		return ret, nil
	}
	if err := dbWrapper.Select(ctx, &ret.Matches, "SELECT "+sourceFileMatchColumns+` FROM source_file_matches
 WHERE file_uuid=? AND generation=? ORDER BY uuid LIMIT 1001`, ret.Removed.UUID, ret.Removed.Generation); err != nil {
		return nil, err
	}
	if len(ret.Matches) > 1000 {
		ret.Matches = nil
		ret.BlockedReason = "source_history_requires_review"
	}
	return ret, nil
}

func deduplicationFile(ctx context.Context, path string) (*models.FileDeduplicationFile, *models.ArchiveEntity, string, error) {
	// Exact literal paths are required; case folding must not select an alias.
	files, err := NewFileStore().FindAllByPath(ctx, path, true)
	if err != nil {
		return nil, nil, "", err
	}
	if len(files) != 1 {
		return nil, nil, "file_requires_intake", nil
	}
	f := files[0]
	if f.Base().ZipFileID != nil {
		return nil, nil, "archive_member_requires_review", nil
	}
	var kind models.ArchiveEntityKind
	switch f.(type) {
	case *models.VideoFile:
		kind = models.ArchiveScene
	case *models.ImageFile:
		kind = models.ArchiveImage
	default:
		return nil, nil, "unsupported_file_type", nil
	}
	entity, err := (&ArchiveEntityStore{}).FindByLocalID(ctx, models.ArchiveFile, int(f.Base().ID))
	if err != nil {
		return nil, nil, "", err
	}
	if entity == nil || entity.State != models.ArchiveEntityActive {
		return nil, nil, "file_requires_intake", nil
	}
	owners, err := (&FileContentStore{}).Owners(ctx, entity.UUID)
	if err != nil {
		return nil, nil, "", err
	}
	if len(owners) != 1 || owners[0].Kind != kind {
		return nil, nil, "ambiguous_or_missing_media_owner", nil
	}
	// Never cascade an unreviewed gallery association or nested archive member.
	var nested bool
	if err := dbWrapper.Get(ctx, &nested, `SELECT EXISTS(SELECT 1 FROM galleries_files WHERE file_id=?)
 OR EXISTS(SELECT 1 FROM files WHERE zip_file_id=?) OR EXISTS(SELECT 1 FROM folders WHERE zip_file_id=?)`, f.Base().ID, f.Base().ID, f.Base().ID); err != nil {
		return nil, nil, "", err
	}
	if nested {
		return nil, nil, "archive_membership_requires_review", nil
	}
	primary, err := NewFileStore().IsPrimary(ctx, f.Base().ID)
	if err != nil {
		return nil, nil, "", err
	}
	metadata, err := json.Marshal(f)
	if err != nil {
		return nil, nil, "", err
	}
	return &models.FileDeduplicationFile{UUID: entity.UUID, Revision: entity.Revision, ID: f.Base().ID,
		Generation: f.Base().Generation, Size: f.Base().Size, ModifiedAt: f.Base().ModTime, Primary: primary, Metadata: metadata}, owners[0], "", nil
}

func (s *FileDeduplicationStore) Find(ctx context.Context, id string) (*models.FileDeduplicationReceipt, error) {
	if normalized, err := archiveUUID(id); err != nil || normalized != id {
		return nil, models.ErrFileDeduplicationInvalid
	}
	var ret models.FileDeduplicationReceipt
	err := dbWrapper.Get(ctx, &ret, fileDeduplicationSelect+" WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ret, err
}

func (s *FileDeduplicationStore) Record(ctx context.Context, state models.FileDeduplicationState, receipt models.FileDeduplicationReceipt) (*models.FileDeduplicationReceipt, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if id, err := archiveUUID(receipt.UUID); err != nil || id != receipt.UUID || !archive.ValidSHA256(receipt.Signature) ||
		!archive.ValidSHA256(receipt.SHA256) || state.BlockedReason != "" || state.Kept == nil || state.Removed == nil || state.Owner == nil ||
		receipt.RootUUID != state.Input.RootUUID || receipt.KeepPath != state.Input.KeepPath || receipt.RemovePath != state.Input.RemovePath ||
		receipt.KeptUUID != state.Kept.UUID || receipt.KeptGeneration != state.Kept.Generation ||
		receipt.RemovedUUID != state.Removed.UUID || receipt.RemovedGeneration != state.Removed.Generation || receipt.MediaUUID != state.Owner.UUID {
		return nil, models.ErrFileDeduplicationInvalid
	}
	prior, err := s.Find(ctx, receipt.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.Signature != receipt.Signature {
			return nil, models.ErrFileDeduplicationReplay
		}
		return prior, nil
	}
	current, err := s.Inspect(ctx, state.Input)
	if err != nil {
		return nil, err
	}
	a, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(a, b) {
		return nil, models.ErrFileDeduplicationConflict
	}
	for _, f := range []*models.FileDeduplicationFile{state.Kept, state.Removed} {
		verified, err := (&FileContentStore{}).Current(ctx, f.UUID)
		if err != nil {
			return nil, err
		}
		if verified == nil || verified.Generation != f.Generation || verified.Content.SHA256 != receipt.SHA256 || verified.Content.Size != f.Size {
			return nil, models.ErrFileContentConflict
		}
	}
	guardFileContent(ctx, state.Kept.UUID, state.Kept.Generation)
	recorded := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !recorded {
			return models.ErrFileDeduplicationConflict
		}
		return nil
	})
	_, err = dbWrapper.Exec(ctx, `INSERT INTO file_deduplications
 (uuid,signature,root_uuid,keep_path,remove_path,kept_file_uuid,kept_generation,removed_file_uuid,removed_generation,media_uuid,sha256,proof)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, receipt.UUID, receipt.Signature, receipt.RootUUID, receipt.KeepPath, receipt.RemovePath,
		receipt.KeptUUID, receipt.KeptGeneration, receipt.RemovedUUID, receipt.RemovedGeneration, receipt.MediaUUID, receipt.SHA256, string(receipt.Proof))
	if err != nil {
		return nil, err
	}
	// Preserve each original assertion and add an explicitly derived match. A
	// reviewed deduplication is byte evidence for this transfer, not a retroactive
	// claim that the source supplied a SHA-256 or that two posts were the same.
	for _, match := range state.Matches {
		details, err := json.Marshal(map[string]string{"deduplication_uuid": receipt.UUID, "previous_match_uuid": match.UUID, "basis": "server-verified-equal-bytes"})
		if err != nil {
			return nil, err
		}
		_, err = (&SourceFileStore{}).RecordMatch(ctx, models.SourceFileMatch{
			UUID:            uuid.NewSHA1(uuid.MustParse(receipt.UUID), []byte("source-match:"+match.UUID)).String(),
			ObservationUUID: match.ObservationUUID, FileUUID: state.Kept.UUID, Generation: state.Kept.Generation,
			Basis: "review", Origin: "review", Details: details,
		})
		if err != nil {
			return nil, err
		}
	}
	if err := (&ArchiveEntityStore{}).Redirect(ctx, state.Removed.UUID, state.Kept.UUID, state.Removed.Revision); err != nil {
		return nil, err
	}
	result, err := s.Find(ctx, receipt.UUID)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	recorded = true
	return result, nil
}
