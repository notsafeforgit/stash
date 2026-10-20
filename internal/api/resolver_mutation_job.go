package api

import "context"

func (r *mutationResolver) StopJob(ctx context.Context, jobID string) (bool, error) {
	err := r.jobs.CancelJob(jobID)
	return err == nil, err
}
func (r *mutationResolver) StopAllJobs(ctx context.Context) (bool, error) {
	err := r.jobs.CancelAll()
	return err == nil, err
}
