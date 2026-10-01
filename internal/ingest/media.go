package ingest

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// PreparedMedia is an uncommitted file inspection, not a completion receipt.
// Its descriptor stays open until publication or rejection. The worker must
// revalidate against the root loaded in its final transaction before committing.
type PreparedMedia struct {
	verified *archive.VerifiedFile
	media    models.File
	rootUUID string
	relative string
}

func (p *PreparedMedia) Close() error                   { return p.verified.Close() }
func (p *PreparedMedia) SHA256() string                 { return p.verified.SHA256 }
func (p *PreparedMedia) Snapshot() archive.FileSnapshot { return p.verified.Snapshot }

func (p *PreparedMedia) File() models.File {
	ret := p.media.Clone()
	ret.Base().Fingerprints = append(models.Fingerprints(nil), p.media.Base().Fingerprints...)
	return ret
}

func (p *PreparedMedia) Revalidate(ctx context.Context, currentRoot models.MediaRoot) error {
	return p.verified.Revalidate(ctx, currentRoot)
}

// RecordContent publishes the inspected byte identity in the caller's write
// transaction. File creation/update must already have used the prepared metadata.
// Keep PreparedMedia open until WithTxn returns: the final hook checks the live
// binding and descriptor after all other domain writes, before SQLite commits.
func (p *PreparedMedia) RecordContent(ctx context.Context, repo models.Repository, fileUUID string, generation int64) (*models.FileContentVerification, error) {
	root, err := repo.MediaRoot.Find(ctx, p.rootUUID)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, models.ErrFileGenerationConflict
	}
	if err := p.Revalidate(ctx, *root); err != nil {
		return nil, err
	}
	proof, err := repo.FileContent.RecordVerification(ctx, models.FileContentInput{
		FileUUID: fileUUID, ExpectedGeneration: generation, SHA256: p.SHA256(), RootUUID: p.rootUUID, RelativePath: p.relative, Snapshot: p.Snapshot(),
		ExpectedRootRevision: root.Revision,
	})
	if err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		current, err := repo.MediaRoot.Find(ctx, p.rootUUID)
		if err != nil {
			return err
		}
		if current == nil {
			return models.ErrFileGenerationConflict
		}
		return p.Revalidate(ctx, *current)
	})
	return proof, nil
}

// PrepareMedia verifies an authorized root-relative file and runs the ordinary
// scanner's fingerprint calculator and decorators against the held descriptor.
// Authentication, collection policy, generation fencing, and durable job/receipt
// publication belong to the caller. No database or library handler runs here.
func PrepareMedia(ctx context.Context, root models.MediaRoot, relative string, expectedSize *int64, expectedSHA256 string, scanner *file.Scanner) (*PreparedMedia, error) {
	if scanner == nil || scanner.FingerprintCalculator == nil || len(scanner.FileDecorators) == 0 {
		return nil, errors.New("media intake requires a configured scanner")
	}
	verified, err := archive.VerifyMediaFile(ctx, root, relative, expectedSize, expectedSHA256)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			verified.Close()
		}
	}()
	path := filepath.Join(root.Binding.Path, filepath.FromSlash(relative))
	// Preparation performs no path lookup in the database. Case sensitivity is
	// relevant only to persistence, where the worker must use the reviewed root.
	pinned, err := file.NewPinnedFileFS(ctx, path, verified.File, true)
	if err != nil {
		return nil, err
	}
	info, err := pinned.Stat(path)
	if err != nil {
		return nil, err
	}
	base := &models.BaseFile{Path: path, Basename: filepath.Base(path), Size: info.Size(), DirEntry: models.DirEntry{ModTime: file.ModTime(info)}}
	media, err := scanner.PrepareFile(ctx, file.ScannedFile{BaseFile: base, FS: pinned, Info: info})
	if err != nil {
		return nil, err
	}
	visual := false
	switch f := media.(type) {
	case *models.ImageFile:
		visual = f.Width > 0 && f.Height > 0
	case *models.VideoFile:
		visual = f.Width > 0 && f.Height > 0 && f.VideoCodec != ""
	}
	if !visual {
		return nil, ErrUnsupported
	}
	if err := verified.Revalidate(ctx, root); err != nil {
		return nil, err
	}
	keep = true
	return &PreparedMedia{verified: verified, media: media, rootUUID: root.UUID, relative: relative}, nil
}
