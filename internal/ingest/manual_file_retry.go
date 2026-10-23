package ingest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// RetryManualFile retains terminal attempts and submits a new durable job.
// Equivalent concurrent retries share that job. Unpublished work still requires
// its original file/policy review; published work resumes only its effects.
func (s *Service) RetryManualFile(ctx context.Context, parentRequest string, revision int64, request string) (*ManualFileStatus, error) {
	if !ValidUUID(parentRequest) || !ValidUUID(request) || revision < 1 || parentRequest == request {
		return nil, ErrInvalid
	}
	var current *models.ArchiveJob
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		parent, err := s.Repo.ArchiveJob.FindSubmission(ctx, parentRequest)
		if err != nil {
			return err
		}
		work, err := decodeManualFileWork(parent)
		if err != nil {
			return err
		}
		previous, err := s.Repo.ArchiveJob.FindSubmission(ctx, request)
		if err != nil {
			return err
		}
		if previous != nil {
			saved, err := decodeManualFileWork(previous)
			if err != nil || saved.Manual.Resume == nil || saved.Manual.Resume.FromJobUUID != parent.UUID || saved.Manual.Resume.FromRevision != revision || !sameManualFileWork(*saved, *work) {
				return models.ErrArchiveJobConflict
			}
			current = previous
			return nil
		}
		if parent.Revision != revision || (parent.State != "failed" && parent.State != "cancelled") {
			return models.ErrArchiveJobConflict
		}
		progress, err := fileProgressForWork(parent, *work)
		if err != nil {
			return err
		}
		resume := &FileResume{FromJobUUID: parent.UUID, FromRevision: revision, Publication: progress.Publication}
		if progress.Publication != nil {
			resume.PublicationJobUUID = parent.UUID
			if work.Manual.Resume != nil && work.Manual.Resume.Publication != nil {
				resume.PublicationJobUUID = work.Manual.Resume.PublicationJobUUID
			}
		}
		work.Manual.Resume = resume
		if err := s.validateManualFileResume(ctx, *work); err != nil {
			return err
		}
		recheck := func(ctx context.Context) error {
			plan, err := s.manualFilePlan(ctx, work.Manual.Request.ManualFileInput)
			if err != nil {
				return err
			}
			if plan.Preview.Signature != work.Manual.Request.Signature {
				return ErrManualFileChanged
			}
			return nil
		}
		if progress.Publication == nil {
			if err := recheck(ctx); err != nil {
				return err
			}
			txn.AddPreCommitHook(ctx, recheck)
		}
		args, err := json.Marshal(work)
		if err != nil {
			return err
		}
		current, err = s.Repo.ArchiveJob.Submit(ctx, models.ArchiveJobSubmission{RequestUUID: request, Kind: models.ArchiveJobVerifyMedia, WorkKey: Digest(args), ResourceKey: parent.ResourceKey, Arguments: args, MaxAttempts: 8}, time.Now(), 10000)
		return err
	})
	if err != nil {
		return nil, err
	}
	return manualFileStatus(current, request)
}
