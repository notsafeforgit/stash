package ingest

import "github.com/stashapp/stash/pkg/models"

// FileJobDescription exposes retained admission/publication references without
// worker settings, filesystem access, permission changes or lease maintenance.
type FileJobDescription struct {
	CollectionUUID     string
	CollectionRevision int
	RelativePath       string
	MediaUUID          string
	ManualRequestUUID  string
	RetryOfJobUUID     string
	Failure            *FileFailure
}

func DescribeFileJob(current *models.ArchiveJob) (*FileJobDescription, error) {
	if current == nil || current.Kind != models.ArchiveJobVerifyMedia {
		return nil, ErrInvalid
	}
	var work FileWork
	if err := StrictJSON(current.Arguments, 262144, &work); err != nil || !validFileWork(work) {
		return nil, ErrInvalid
	}
	progress, err := fileProgressForWork(current, work)
	if err != nil {
		return nil, err
	}
	ret := &FileJobDescription{CollectionUUID: work.Publication.CollectionUUID, CollectionRevision: work.Publication.CollectionRevision, RelativePath: work.Publication.Target.RelativePath}
	var completion fileCompletion
	if err := StrictJSON(current.Result, 16384, &completion); err != nil {
		return nil, err
	}
	ret.Failure = completion.Failure
	if resume := fileResume(work); resume != nil {
		ret.RetryOfJobUUID = resume.FromJobUUID
	}
	if progress.Publication != nil {
		ret.MediaUUID = progress.Publication.MediaUUID
	}
	if work.Manual != nil {
		ret.ManualRequestUUID = work.Manual.Request.RequestUUID
	}
	return ret, nil
}
