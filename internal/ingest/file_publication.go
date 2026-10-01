package ingest

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// FileTarget is server-created intake state, never a producer-supplied override.
// Persist it with accepted work before probing. It protects both an existing
// file's lifetime and an initially empty path against delayed resurrection.
type FileTarget struct {
	RootUUID     string               `json:"root_uuid"`
	RelativePath string               `json:"relative_path"`
	PathFence    models.FilePathFence `json:"path_fence"`
	FileUUID     string               `json:"file_uuid,omitempty"`
	Generation   int64                `json:"generation"`
}

type PublishedFile struct {
	File     models.File
	Identity *models.ArchiveEntity
	Proof    *models.FileContentVerification
	Created  bool
}

// CaptureFileTarget runs in the accepting transaction after collection/root
// authorization. Filesystem case sensitivity is resolved by the server. No
// filesystem writes, hashing, or handlers run here.
func CaptureFileTarget(ctx context.Context, repo models.Repository, root models.MediaRoot, relative string, caseSensitive bool) (*FileTarget, error) {
	if !ValidUUID(root.UUID) || root.State != "active" || root.Binding == nil || !archive.ValidRootRelativePath(relative, false) {
		return nil, ErrDefinition
	}
	path := filepath.Join(root.Binding.Path, filepath.FromSlash(relative))
	fence, err := repo.FilePath.Snapshot(ctx, path, caseSensitive)
	if err != nil {
		return nil, err
	}
	target := &FileTarget{RootUUID: root.UUID, RelativePath: relative, PathFence: *fence}
	current, err := repo.File.FindByPath(ctx, path, caseSensitive)
	if err != nil || current == nil {
		return target, err
	}
	if current.Base().ZipFileID != nil {
		return nil, ErrUnsupported
	}
	identity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveFile, int(current.Base().ID))
	if err != nil {
		return nil, err
	}
	if identity == nil || identity.State != models.ArchiveEntityActive {
		return nil, models.ErrFileGenerationConflict
	}
	target.FileUUID, target.Generation = identity.UUID, current.Base().Generation
	return target, nil
}

// PublishFile persists prepared metadata and its byte proof in the caller's
// transaction, without fingerprint matching or library handler side effects.
// A concurrent ordinary scan may have created the same path; use that row only
// while the removal fence is unchanged. Scene/image/provenance/receipt writes
// belong in this same transaction, and PreparedMedia stays open through commit.
func (p *PreparedMedia) PublishFile(ctx context.Context, repo models.Repository, target FileTarget) (*PublishedFile, error) {
	if !txn.HasHooks(ctx) {
		return nil, errors.New("file publication requires a managed transaction")
	}
	if target.RootUUID != p.rootUUID || target.RelativePath != p.relative || target.PathFence.Path != p.media.Base().Path ||
		(target.FileUUID == "" && target.Generation != 0) || (target.FileUUID != "" && (!ValidUUID(target.FileUUID) || target.Generation < 1)) {
		return nil, models.ErrFileGenerationConflict
	}
	if err := repo.FilePath.Check(ctx, target.PathFence); err != nil {
		return nil, err
	}
	root, err := repo.MediaRoot.Find(ctx, p.rootUUID)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, ErrDefinition
	}
	if err := p.Revalidate(ctx, *root); err != nil {
		return nil, err
	}
	existing, err := repo.File.FindByPath(ctx, target.PathFence.Path, target.PathFence.CaseSensitive)
	if err != nil {
		return nil, err
	}
	if target.FileUUID != "" {
		identity, err := repo.ArchiveEntity.Resolve(ctx, target.FileUUID)
		if err != nil {
			return nil, err
		}
		if existing == nil || identity == nil || identity.Kind != models.ArchiveFile || identity.State != models.ArchiveEntityActive || identity.LocalID == nil ||
			models.FileID(*identity.LocalID) != existing.Base().ID || target.Generation != existing.Base().Generation {
			return nil, models.ErrFileGenerationConflict
		}
	}
	media := p.File()
	base := media.Base()
	created := existing == nil
	if created {
		// A newly delivered completion cannot recreate a removed path. An
		// explicit restore or ordinary scan must establish its new file lifetime.
		if target.PathFence.Revision != 0 {
			return nil, models.ErrFilePathChanged
		}
		folder, err := file.GetOrCreateFolderHierarchy(ctx, repo.Folder, filepath.Dir(base.Path), []string{root.Binding.Path})
		if err != nil {
			return nil, err
		}
		base.ParentFolderID = folder.ID
		base.CreatedAt, base.UpdatedAt = time.Now(), time.Now()
		if err := repo.File.Create(ctx, media); err != nil {
			return nil, err
		}
	} else {
		old := existing.Base()
		if old.ZipFileID != nil || (reflect.TypeOf(existing) != reflect.TypeOf(media) && reflect.TypeOf(existing) != reflect.TypeFor[*models.BaseFile]()) {
			return nil, ErrUnsupported
		}
		identity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveFile, int(old.ID))
		if err != nil {
			return nil, err
		}
		if identity == nil {
			return nil, models.ErrFileGenerationConflict
		}
		proof, err := repo.FileContent.Current(ctx, identity.UUID)
		if err != nil {
			return nil, err
		}
		if proof != nil && (proof.Content.SHA256 != p.SHA256() || proof.Content.Size != p.Snapshot().Size) {
			return nil, models.ErrFileContentConflict
		}
		if proof != nil {
			// Reinspection of the same verified bytes must not discard generated
			// fingerprints, such as perceptual hashes, absent from preparation.
			calculated := base.Fingerprints
			base.Fingerprints = append(models.Fingerprints(nil), old.Fingerprints...)
			base.SetFingerprints(calculated)
		}
		base.ID, base.Generation, base.ParentFolderID = old.ID, old.Generation, old.ParentFolderID
		base.Basename, base.Path = old.Basename, old.Path
		base.CreatedAt, base.UpdatedAt = old.CreatedAt, time.Now()
		if err := repo.File.Update(ctx, media); err != nil {
			return nil, err
		}
	}
	identity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveFile, int(base.ID))
	if err != nil {
		return nil, err
	}
	if identity == nil {
		return nil, models.ErrFileGenerationConflict
	}
	relative, err := filepath.Rel(root.Binding.Path, base.Path)
	if err != nil || !archive.ValidRootRelativePath(filepath.ToSlash(relative), false) {
		return nil, models.ErrFileGenerationConflict
	}
	proof, err := p.recordContentAt(ctx, repo, identity.UUID, base.Generation, filepath.ToSlash(relative))
	if err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error { return repo.FilePath.Check(ctx, target.PathFence) })
	return &PublishedFile{File: media, Identity: identity, Proof: proof, Created: created}, nil
}
