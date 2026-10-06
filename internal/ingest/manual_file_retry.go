package ingest

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// The request on ManualFileWork always identifies the original review. Retry
// receipts may coalesce onto one new job; none replaces its publication/event ID.
type ManualFileResume struct {
	FromJobUUID        string                   `json:"from_job_uuid"`
	FromRevision       int64                    `json:"from_revision"`
	PublicationJobUUID string                   `json:"publication_job_uuid,omitempty"`
	Publication        *IntakePublicationResult `json:"publication,omitempty"`
}

func validManualFileResume(resume *ManualFileResume) bool {
	if resume == nil {
		return true
	}
	return ValidUUID(resume.FromJobUUID) && resume.FromRevision > 0 &&
		((resume.Publication == nil && resume.PublicationJobUUID == "") ||
			(resume.Publication != nil && ValidUUID(resume.PublicationJobUUID) && ValidUUID(resume.Publication.FileUUID) && ValidUUID(resume.Publication.MediaUUID) && ValidUUID(resume.Publication.ContentUUID) && resume.Publication.Generation > 0))
}

func fileProgressForWork(current *models.ArchiveJob, work FileWork) (fileProgress, error) {
	var progress fileProgress
	if err := StrictJSON(current.Progress, 16384, &progress); err != nil || (progress.Version != 0 && progress.Version != 1) ||
		(progress.Version == 0 && progress.Publication != nil) || (progress.Version == 1 && progress.Publication == nil) {
		return progress, ErrInvalid
	}
	if work.Manual != nil && work.Manual.Resume != nil && work.Manual.Resume.Publication != nil {
		saved := work.Manual.Resume.Publication
		if progress.Publication != nil && !reflect.DeepEqual(progress.Publication, saved) {
			return progress, ErrInvalid
		}
		progress = fileProgress{Version: 1, Publication: saved}
	}
	return progress, nil
}

func sameManualFileWork(a, b FileWork) bool {
	if a.Manual == nil || b.Manual == nil {
		return false
	}
	left, right := *a.Manual, *b.Manual
	left.Resume, right.Resume = nil, nil
	a.Manual, b.Manual = &left, &right
	return reflect.DeepEqual(a, b)
}

func (s *Service) validateManualFileResume(ctx context.Context, work FileWork) error {
	resume := work.Manual.Resume
	if resume == nil {
		return nil
	}
	original, err := s.Repo.ArchiveJob.FindSubmission(ctx, work.Manual.Request.RequestUUID)
	if err != nil {
		return err
	}
	origin, err := decodeManualFileWork(original)
	if err != nil || origin.Manual.Resume != nil || !sameManualFileWork(*origin, work) {
		return ErrInvalid
	}
	parent, err := s.Repo.ArchiveJob.Find(ctx, resume.FromJobUUID)
	if err != nil {
		return err
	}
	if parent == nil || parent.Revision != resume.FromRevision || (parent.State != "failed" && parent.State != "cancelled") {
		return ErrInvalid
	}
	prior, err := decodeManualFileWork(parent)
	if err != nil || !sameManualFileWork(*prior, work) {
		return ErrInvalid
	}
	progress, err := fileProgressForWork(parent, *prior)
	if err != nil || !reflect.DeepEqual(progress.Publication, resume.Publication) {
		return ErrInvalid
	}
	if resume.Publication != nil {
		// Verify the actual domain checkpoint directly; do not walk an unbounded
		// chain of retries or accept an invented publication in job arguments.
		published, err := s.Repo.ArchiveJob.Find(ctx, resume.PublicationJobUUID)
		if err != nil {
			return err
		}
		publishedWork, err := decodeManualFileWork(published)
		if err != nil || !sameManualFileWork(*publishedWork, work) ||
			(publishedWork.Manual.Resume != nil && publishedWork.Manual.Resume.Publication != nil) {
			return ErrInvalid
		}
		var checkpoint fileProgress
		if err := StrictJSON(published.Progress, 16384, &checkpoint); err != nil || checkpoint.Version != 1 || !reflect.DeepEqual(checkpoint.Publication, resume.Publication) {
			return ErrInvalid
		}
	}
	return nil
}

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
		resume := &ManualFileResume{FromJobUUID: parent.UUID, FromRevision: revision, Publication: progress.Publication}
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
