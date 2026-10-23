package ingest

import (
	"context"
	"reflect"

	"github.com/stashapp/stash/pkg/models"
)

// FileResume retains terminal parent and publication checkpoints. Retry receipts
// may coalesce onto one job; none replaces the original publication/event ID.
type FileResume struct {
	FromJobUUID        string                   `json:"from_job_uuid"`
	FromRevision       int64                    `json:"from_revision"`
	PublicationJobUUID string                   `json:"publication_job_uuid,omitempty"`
	Publication        *IntakePublicationResult `json:"publication,omitempty"`
}

func fileResume(work FileWork) *FileResume {
	if work.Manual != nil {
		return work.Manual.Resume
	}
	return work.Resume
}

func validFileResume(resume *FileResume) bool {
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
	if resume := fileResume(work); resume != nil && resume.Publication != nil {
		saved := resume.Publication
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
	return sameFileWork(a, b)
}

func sameFileWork(a, b FileWork) bool {
	a.Resume, b.Resume = nil, nil
	if a.Manual != nil {
		m := *a.Manual
		m.Resume = nil
		a.Manual = &m
	}
	if b.Manual != nil {
		m := *b.Manual
		m.Resume = nil
		b.Manual = &m
	}
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
	return s.validateFileResume(ctx, work, original)
}

func (s *Service) validateFileResume(ctx context.Context, work FileWork, original *models.ArchiveJob) error {
	resume := fileResume(work)
	if resume == nil {
		return ErrInvalid
	}
	origin, err := decodeFileWork(original)
	if err != nil || fileResume(*origin) != nil || !sameFileWork(*origin, work) {
		return ErrInvalid
	}
	parent, err := s.Repo.ArchiveJob.Find(ctx, resume.FromJobUUID)
	if err != nil {
		return err
	}
	if parent == nil || parent.Revision != resume.FromRevision || (parent.State != "failed" && parent.State != "cancelled") {
		return ErrInvalid
	}
	prior, err := decodeFileWork(parent)
	if err != nil || !sameFileWork(*prior, work) {
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
		publishedWork, err := decodeFileWork(published)
		if err != nil || !sameFileWork(*publishedWork, work) ||
			(fileResume(*publishedWork) != nil && fileResume(*publishedWork).Publication != nil) {
			return ErrInvalid
		}
		var checkpoint fileProgress
		if err := StrictJSON(published.Progress, 16384, &checkpoint); err != nil || checkpoint.Version != 1 || !reflect.DeepEqual(checkpoint.Publication, resume.Publication) {
			return ErrInvalid
		}
	}
	return nil
}
