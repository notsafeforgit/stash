package api

import (
	"context"

	"github.com/stashapp/stash/pkg/job"
)

func (r *queryResolver) JobQueue(ctx context.Context) ([]*Job, error) {
	queue := r.jobs.GetQueue()

	var ret []*Job
	for _, j := range queue {
		ret = append(ret, jobToJobModel(j))
	}

	return ret, nil
}

func (r *queryResolver) FindJob(ctx context.Context, input FindJobInput) (*Job, error) {
	j, err := r.jobs.Find(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, nil
	}

	return jobToJobModel(*j), nil
}

func jobToJobModel(j job.Job) *Job {
	subTasks := make([]string, len(j.Details))
	for i, t := range j.Details {
		subTasks[i] = sanitiseWebsocketString(t)
	}

	var jobError *string
	if j.Error != nil {
		s := sanitiseWebsocketString(*j.Error)
		jobError = &s
	}

	ret := &Job{
		ID:          j.ID,
		Status:      JobStatus(j.Status),
		Description: sanitiseWebsocketString(j.Description),
		SubTasks:    subTasks,
		StartTime:   j.StartTime,
		EndTime:     j.EndTime,
		AddTime:     j.AddTime,
		Error:       jobError,
	}

	if j.Progress != -1 {
		ret.Progress = &j.Progress
	}

	return ret
}

func (r *queryResolver) JobHistory(ctx context.Context, limit int, offset int) ([]*Job, error) {
	history, err := r.jobs.History(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]*Job, 0, len(history))
	for _, j := range history {
		result = append(result, jobToJobModel(j))
	}
	return result, nil
}

type jobService interface {
	Find(context.Context, string) (*job.Job, error)
	History(context.Context, int, int) ([]job.Job, error)
	GetQueue() []job.Job
	CancelJob(string) error
	CancelAll() error
	Subscribe(context.Context) *job.ManagerSubscription
}
