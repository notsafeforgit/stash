// Package dedup removes verified redundant locations without merging logical
// media or discarding source provenance. Candidate discovery is host-owned;
// only this service stages filesystem removal and commits native associations.
package dedup

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type Service struct {
	repo  models.Repository
	trash string
}

func New(repo models.Repository, trash string) *Service { return &Service{repo: repo, trash: trash} }

type Request struct {
	models.FileDeduplicationInput
	RequestUUID string `json:"request_uuid"`
	Signature   string `json:"signature"`
}

type Preview struct {
	models.FileDeduplicationInput
	Eligible       bool   `json:"eligible"`
	BlockedReason  string `json:"blocked_reason,omitempty"`
	Signature      string `json:"signature"`
	RootRevision   int    `json:"root_revision"`
	MediaUUID      string `json:"media_uuid,omitempty"`
	KeptUUID       string `json:"kept_file_uuid,omitempty"`
	RemovedUUID    string `json:"removed_file_uuid,omitempty"`
	Bytes          int64  `json:"bytes"`
	SourceMatches  int    `json:"source_matches"`
	ReplacePrimary bool   `json:"replace_primary"`
}

type plan = archive.DeduplicationProof

func (s *Service) inspect(ctx context.Context, input models.FileDeduplicationInput) (*plan, error) {
	state, err := s.repo.FileDeduplication.Inspect(ctx, input)
	if err != nil {
		return nil, err
	}
	p := &plan{Version: 1, State: *state}
	if state.BlockedReason != "" {
		return p, nil
	}
	p.Keep, err = archive.InspectMediaFile(ctx, state.Root, input.KeepPath)
	if err != nil {
		return nil, err
	}
	p.Remove, err = archive.InspectMediaFile(ctx, state.Root, input.RemovePath)
	if err != nil {
		return nil, err
	}
	for i, f := range []*models.FileDeduplicationFile{state.Kept, state.Removed} {
		inspection := []*archive.MediaFileInspection{p.Keep, p.Remove}[i]
		if inspection.Snapshot.Size != f.Size || inspection.Snapshot.ModifiedAt.Unix() != f.ModifiedAt.Unix() {
			p.State.BlockedReason = "file_requires_rescan"
		}
	}
	if p.Keep.Snapshot.Identity == p.Remove.Snapshot.Identity {
		p.State.BlockedReason = "same_filesystem_entry"
	}
	return p, nil
}

// Preview is read-only. Full byte verification runs at Apply, outside the writer
// transaction; a candidate finder's hash is never accepted as deletion proof.
func (s *Service) Preview(ctx context.Context, input models.FileDeduplicationInput) (*Preview, error) {
	var p *plan
	err := s.repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		p, err = s.inspect(ctx, input)
		return err
	})
	if err != nil {
		return nil, err
	}
	signature, err := p.Signature()
	if err != nil {
		return nil, err
	}
	ret := &Preview{FileDeduplicationInput: input, Signature: signature, Eligible: p.State.BlockedReason == "",
		BlockedReason: p.State.BlockedReason, RootRevision: p.State.Root.Revision, SourceMatches: len(p.State.Matches)}
	if p.State.Kept != nil {
		ret.KeptUUID = p.State.Kept.UUID
	}
	if p.State.Removed != nil {
		ret.RemovedUUID, ret.Bytes, ret.ReplacePrimary = p.State.Removed.UUID, p.State.Removed.Size, p.State.Removed.Primary
	}
	if p.State.Owner != nil {
		ret.MediaUUID = p.State.Owner.UUID
	}
	return ret, nil
}

func (s *Service) check(ctx context.Context, request Request) (*plan, error) {
	p, err := s.inspect(ctx, request.FileDeduplicationInput)
	if err != nil {
		return nil, err
	}
	signature, err := p.Signature()
	if err != nil {
		return nil, err
	}
	if p.State.BlockedReason != "" || signature != request.Signature {
		return nil, models.ErrFileDeduplicationConflict
	}
	return p, nil
}

func (s *Service) prior(ctx context.Context, request Request) (*models.FileDeduplicationReceipt, error) {
	prior, err := s.repo.FileDeduplication.Find(ctx, request.RequestUUID)
	if err != nil {
		return nil, err
	}
	if prior != nil && (prior.Signature != request.Signature || prior.RootUUID != request.RootUUID || prior.KeepPath != request.KeepPath || prior.RemovePath != request.RemovePath) {
		return nil, models.ErrFileDeduplicationReplay
	}
	return prior, nil
}

func (s *Service) Apply(ctx context.Context, request Request) (*models.FileDeduplicationReceipt, error) {
	if id, err := uuid.Parse(request.RequestUUID); err != nil || id == uuid.Nil || id.String() != request.RequestUUID || !archive.ValidSHA256(request.Signature) {
		return nil, models.ErrFileDeduplicationInvalid
	}
	var p *plan
	var result *models.FileDeduplicationReceipt
	err := s.repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.prior(ctx, request)
		if err != nil || result != nil {
			return err
		}
		p, err = s.check(ctx, request)
		return err
	})
	if err != nil || result != nil {
		return result, err
	}
	kept, err := archive.VerifyMediaFile(ctx, p.State.Root, request.KeepPath, &p.State.Kept.Size, p.Keep.SHA256)
	if err != nil {
		return nil, err
	}
	defer kept.Close()
	removed, err := archive.VerifyMediaFile(ctx, p.State.Root, request.RemovePath, &p.State.Removed.Size, kept.SHA256)
	if err != nil {
		return nil, err
	}
	defer removed.Close()
	if !p.Keep.Matches(kept.Snapshot, kept.SHA256) || !p.Remove.Matches(removed.Snapshot, removed.SHA256) {
		return nil, models.ErrFileDeduplicationConflict
	}
	// Verification is independently useful evidence. Commit it first, while both
	// original descriptors and paths are intact; its generation guards must not
	// be weakened to allow a deleted file in the subsequent operation.
	err = s.repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.prior(ctx, request)
		if err != nil || result != nil {
			return err
		}
		if _, err := s.check(ctx, request); err != nil {
			return err
		}
		for i, f := range []*models.FileDeduplicationFile{p.State.Kept, p.State.Removed} {
			verified := []*archive.VerifiedFile{kept, removed}[i]
			relative := []string{request.KeepPath, request.RemovePath}[i]
			if err := verified.Revalidate(ctx, p.State.Root); err != nil {
				return err
			}
			_, err := s.repo.FileContent.RecordVerification(ctx, models.FileContentInput{FileUUID: f.UUID, ExpectedGeneration: f.Generation,
				SHA256: verified.SHA256, RootUUID: request.RootUUID, ExpectedRootRevision: p.State.Root.Revision, RelativePath: relative, Snapshot: verified.Snapshot})
			if err != nil {
				return err
			}
		}
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error { return s.validate(ctx, p.State.Root, kept, removed) })
		return nil
	})
	if err != nil || result != nil {
		return result, err
	}
	proof, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var staged *archive.VerifiedFile
	defer func() {
		if staged != nil {
			_ = staged.Close()
		}
	}()
	err = s.repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.prior(ctx, request)
		if err != nil || result != nil {
			return err
		}
		if _, err := s.check(ctx, request); err != nil {
			return err
		}
		if err := s.validate(ctx, p.State.Root, kept, removed); err != nil {
			return err
		}
		deleter := file.NewDeleterWithTrash(s.trash)
		deleter.RegisterHooks(ctx)
		result, err = s.repo.FileDeduplication.Record(ctx, p.State, models.FileDeduplicationReceipt{UUID: request.RequestUUID,
			Signature: request.Signature, RootUUID: request.RootUUID, KeepPath: request.KeepPath, RemovePath: request.RemovePath,
			KeptUUID: p.State.Kept.UUID, KeptGeneration: p.State.Kept.Generation, RemovedUUID: p.State.Removed.UUID,
			RemovedGeneration: p.State.Removed.Generation, MediaUUID: p.State.Owner.UUID, SHA256: kept.SHA256, Proof: proof})
		if err != nil {
			return err
		}
		if p.State.Removed.Primary {
			if p.State.Owner.Kind == models.ArchiveScene {
				partial := models.NewScenePartial()
				partial.PrimaryFileID = &p.State.Kept.ID
				_, err = s.repo.Scene.UpdatePartial(ctx, *p.State.Owner.LocalID, partial)
			} else {
				partial := models.NewImagePartial()
				partial.PrimaryFileID = &p.State.Kept.ID
				_, err = s.repo.Image.UpdatePartial(ctx, *p.State.Owner.LocalID, partial)
			}
			if err != nil {
				return err
			}
		}
		path := filepath.Join(p.State.Root.Binding.Path, filepath.FromSlash(request.RemovePath))
		if err := deleter.FileWithValidation(path, func(moved string) error {
			relative, err := filepath.Rel(p.State.Root.Binding.Path, moved)
			if err != nil {
				return err
			}
			// Rename changes ctime. Hash the staged entry rather than dropping the
			// change-token guard and trusting whichever bytes were moved.
			staged, err = archive.VerifyMediaFile(ctx, p.State.Root, filepath.ToSlash(relative), &removed.Snapshot.Size, kept.SHA256)
			if err != nil {
				return err
			}
			if staged.Snapshot.Identity != removed.Snapshot.Identity || !staged.Snapshot.ModifiedAt.Equal(removed.Snapshot.ModifiedAt) {
				return archive.ErrMediaFileChanged
			}
			return nil
		}); err != nil {
			return err
		}
		if err := s.repo.File.Destroy(ctx, p.State.Removed.ID); err != nil {
			return err
		}
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error { return s.validate(ctx, p.State.Root, kept, staged) })
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) validate(ctx context.Context, expected models.MediaRoot, files ...*archive.VerifiedFile) error {
	root, err := s.repo.MediaRoot.Find(ctx, expected.UUID)
	if err != nil {
		return err
	}
	if root == nil || root.Revision != expected.Revision {
		return models.ErrFileDeduplicationConflict
	}
	for _, f := range files {
		if f == nil {
			return errors.New("missing deduplication file verification")
		}
		if err := f.Revalidate(ctx, *root); err != nil {
			return err
		}
	}
	return nil
}
