package ingest

import (
	"context"
	"encoding/json"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// FileRetryStatus is application-authenticated recovery of a producer's failed
// verification. Original producer receipts and terminal jobs stay immutable.
type FileRetryStatus struct {
	RequestUUID  string `json:"request_uuid"`
	JobUUID      string `json:"job_uuid"`
	FromJobUUID  string `json:"from_job_uuid"`
	FromRevision int64  `json:"from_revision"`
	Revision     int64  `json:"revision"`
	State        string `json:"state"`
	ErrorCode    string `json:"error_code,omitempty"`
	fileCompletion
}

func fileRetryStatus(current *models.ArchiveJob, request string) (*FileRetryStatus, error) {
	work, err := decodeFileWork(current)
	if err != nil || work.Manual != nil || work.Resume == nil {
		return nil, ErrNotFound
	}
	progress, err := fileProgressForWork(current, *work)
	if err != nil {
		return nil, err
	}
	ret := &FileRetryStatus{RequestUUID: request, JobUUID: current.UUID, FromJobUUID: work.Resume.FromJobUUID, FromRevision: work.Resume.FromRevision,
		Revision: current.Revision, State: current.State, ErrorCode: current.ErrorCode}
	if err := StrictJSON(current.Result, 16384, &ret.fileCompletion); err != nil {
		return nil, err
	}
	ret.RegistrationCommitted, ret.Publication = progress.Publication != nil, progress.Publication
	return ret, nil
}

func (s *Service) FileRetryStatus(ctx context.Context, request string) (*FileRetryStatus, error) {
	if !ValidUUID(request) {
		return nil, ErrInvalid
	}
	var result *FileRetryStatus
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		current, err := s.Repo.ArchiveJob.FindSubmission(ctx, request)
		if err != nil {
			return err
		}
		result, err = fileRetryStatus(current, request)
		return err
	})
	return result, err
}

// RetryFile admits only the exact retained work, with a new durable retry
// receipt. The worker rechecks its original scope, claims, removal fence and
// source choices. It never reruns committed registration to recover effects.
func (s *Service) RetryFile(ctx context.Context, parentID string, revision int64, request string) (*FileRetryStatus, error) {
	if !ValidUUID(parentID) || !ValidUUID(request) || parentID == request || revision < 1 {
		return nil, ErrInvalid
	}
	var current *models.ArchiveJob
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		parent, err := s.Repo.ArchiveJob.Find(ctx, parentID)
		if err != nil {
			return err
		}
		work, err := decodeFileWork(parent)
		if err != nil || work.Manual != nil {
			return ErrNotFound
		}
		previous, err := s.Repo.ArchiveJob.FindSubmission(ctx, request)
		if err != nil {
			return err
		}
		if previous != nil {
			saved, err := decodeFileWork(previous)
			if err != nil || saved.Resume == nil || saved.Resume.FromJobUUID != parentID || saved.Resume.FromRevision != revision || !sameFileWork(*saved, *work) {
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
		resume := &FileResume{FromJobUUID: parentID, FromRevision: revision, Publication: progress.Publication}
		if progress.Publication != nil {
			resume.PublicationJobUUID = parentID
			if work.Resume != nil && work.Resume.Publication != nil {
				resume.PublicationJobUUID = work.Resume.PublicationJobUUID
			}
		}
		work.Resume = resume
		worker := FileWorker{Service: s}
		if _, err := worker.authorizeInTxn(ctx, parent, *work); err != nil {
			return err
		}
		args, err := json.Marshal(work)
		if err != nil {
			return err
		}
		current, err = s.Repo.ArchiveJob.Submit(ctx, models.ArchiveJobSubmission{RequestUUID: request, Kind: models.ArchiveJobVerifyMedia,
			WorkKey: Digest(args), ResourceKey: parent.ResourceKey, Arguments: args, MaxAttempts: parent.MaxAttempts}, time.Now(), 10000)
		return err
	})
	if err != nil {
		return nil, err
	}
	return fileRetryStatus(current, request)
}
