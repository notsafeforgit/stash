package api

import "context"

func (r *mutationResolver) StopJob(ctx context.Context, jobID string) (bool, error) {
	r.jobs.CancelJob(jobID)
	return true, nil
}
func (r *mutationResolver) StopAllJobs(ctx context.Context) (bool, error) {
	r.jobs.CancelAll()
	return true, nil
}
