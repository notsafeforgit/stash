package api

import (
	"context"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/job"
)

func makeJobStatusUpdate(t JobStatusUpdateType, j job.Job) *JobStatusUpdate {
	return &JobStatusUpdate{
		Type: t,
		Job:  jobToJobModel(j),
	}
}

// JobsSubscribe retains the legacy combined stream and its buffer size.
func (r *subscriptionResolver) JobsSubscribe(ctx context.Context) (<-chan *JobStatusUpdate, error) {
	subscription := manager.GetInstance().JobManager.Subscribe(ctx)
	return forwardJobUpdates(ctx, subscription.NewJob, subscription.RemovedJob, subscription.UpdatedJob, 100, false), nil
}

// JobsLifecycleSubscribe isolates lifecycle events from progress traffic.
func (r *subscriptionResolver) JobsLifecycleSubscribe(ctx context.Context) (<-chan *JobStatusUpdate, error) {
	subscription := manager.GetInstance().JobManager.Subscribe(ctx)
	return forwardJobUpdates(ctx, subscription.NewJob, subscription.RemovedJob, nil, 1000, false), nil
}

// JobsProgressSubscribe drops progress ticks when the consumer is slow.
func (r *subscriptionResolver) JobsProgressSubscribe(ctx context.Context) (<-chan *JobStatusUpdate, error) {
	subscription := manager.GetInstance().JobManager.Subscribe(ctx)
	return forwardJobUpdates(ctx, nil, nil, subscription.UpdatedJob, 100, true), nil
}

func (r *subscriptionResolver) ScanCompleteSubscribe(ctx context.Context) (<-chan bool, error) {
	return manager.GetInstance().ScanSubscribe(ctx), nil
}
