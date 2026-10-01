package ingest

import (
	"context"
	"errors"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

var ErrAmbiguousMedia = errors.New("verified media matches multiple library items")
var ErrMediaKindConflict = errors.New("verified media matches a different library item kind")

type PublishedMedia struct {
	File       *PublishedFile
	Media      *models.ArchiveEntity
	Created    bool
	FileLinked bool
}

// PublishMedia uses an existing file owner or a unique verified-byte match.
// Matching fingerprints alone never authorize a native media association.
// Existing ambiguous media identities stay separate for explicit review.
// The caller applies collection/source metadata, galleries, notifications, and
// its completion result in this same managed transaction.
func (p *PreparedMedia) PublishMedia(ctx context.Context, repo models.Repository, target FileTarget, kind models.ArchiveEntityKind) (*PublishedMedia, error) {
	return p.publishMedia(ctx, repo, target, kind, nil)
}

// A selected source attachment may identify the intended media before any file
// arrives. Its canonical target is read in this transaction, never supplied by
// a producer as an override. Existing file owners are not moved or replaced.
func (p *PreparedMedia) publishMedia(ctx context.Context, repo models.Repository, target FileTarget, kind models.ArchiveEntityKind, selected *models.ArchiveEntity) (*PublishedMedia, error) {
	if kind != models.ArchiveScene && kind != models.ArchiveImage {
		return nil, ErrUnsupported
	}
	if _, video := p.media.(*models.VideoFile); kind == models.ArchiveScene && !video {
		return nil, ErrUnsupported
	}
	if selected != nil && (selected.State != models.ArchiveEntityActive || selected.Kind != kind || selected.LocalID == nil) {
		return nil, ErrMediaKindConflict
	}
	published, err := p.PublishFile(ctx, repo, target)
	if err != nil {
		return nil, err
	}
	owners, err := repo.FileContent.Owners(ctx, published.Identity.UUID)
	if err != nil {
		return nil, err
	}
	linked := len(owners) > 0
	selectedOwner := false
	if selected != nil {
		owns, err := repo.FileContent.HasOwner(ctx, published.Identity.UUID, selected.UUID)
		if err != nil {
			return nil, err
		}
		if owns || !linked {
			owners, selectedOwner = []*models.ArchiveEntity{selected}, true
		}
	}
	if len(owners) == 0 {
		owners, err = repo.FileContent.MediaCandidates(ctx, published.Proof.Content.UUID)
		if err != nil {
			return nil, err
		}
	}
	if len(owners) > 1 {
		return nil, ErrAmbiguousMedia
	}
	ret := &PublishedMedia{File: published}
	fileID := published.File.Base().ID
	var localID int
	if len(owners) == 1 {
		owner := owners[0]
		if owner.Kind != kind || owner.LocalID == nil {
			return nil, ErrMediaKindConflict
		}
		localID = *owner.LocalID
		if !linked {
			if kind == models.ArchiveScene {
				err = repo.Scene.AddFileID(ctx, localID, fileID)
			} else {
				err = repo.Image.AddFileID(ctx, localID, fileID)
			}
			if err != nil {
				return nil, err
			}
			ret.FileLinked = true
		}
	} else {
		if kind == models.ArchiveScene {
			media := models.NewScene()
			if err := repo.Scene.Create(ctx, &media, []models.FileID{fileID}); err != nil {
				return nil, err
			}
			localID = media.ID
		} else {
			media := models.NewImage()
			if err := repo.Image.Create(ctx, &models.CreateImageInput{Image: &media, FileIDs: []models.FileID{fileID}}); err != nil {
				return nil, err
			}
			localID = media.ID
		}
		ret.Created, ret.FileLinked = true, true
	}
	ret.Media, err = repo.ArchiveEntity.FindByLocalID(ctx, kind, localID)
	if err != nil {
		return nil, err
	}
	if ret.Media == nil {
		return nil, models.ErrArchiveIdentityConflict
	}
	mediaUUID, fileUUID := ret.Media.UUID, published.Identity.UUID
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		identity, err := repo.ArchiveEntity.Resolve(ctx, mediaUUID)
		if err != nil {
			return err
		}
		if identity == nil || identity.State != models.ArchiveEntityActive || identity.Kind != kind {
			return models.ErrArchiveIdentityConflict
		}
		if selectedOwner {
			owns, err := repo.FileContent.HasOwner(ctx, fileUUID, identity.UUID)
			if err != nil {
				return err
			}
			if !owns {
				return models.ErrArchiveIdentityConflict
			}
			return nil
		}
		owners, err := repo.FileContent.Owners(ctx, fileUUID)
		if err != nil {
			return err
		}
		if len(owners) != 1 || owners[0].UUID != identity.UUID {
			return models.ErrArchiveIdentityConflict
		}
		return nil
	})
	return ret, nil
}
