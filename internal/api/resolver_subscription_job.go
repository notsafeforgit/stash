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

// JobsSubscribe is the legacy combined subscription. All event types flow
// through a single 100-slot channel, which means a UPDATE storm (e.g. a
// scan firing progress ticks faster than the WebSocket can drain them)
// can fill the channel and cause the upstream subscription buffers to
// back up; eventually `notifyNewJob` / `notifyJobRemoved` drop events
// because their non-blocking sends fall through. Clients that depend on
// never missing an ADD or REMOVE should use `JobsLifecycleSubscribe` /
// `JobsProgressSubscribe` instead.
func (r *subscriptionResolver) JobsSubscribe(ctx context.Context) (<-chan *JobStatusUpdate, error) {
	msg := make(chan *JobStatusUpdate, 100)

	subscription := manager.GetInstance().JobManager.Subscribe(ctx)

	go func() {
		for {
			select {
			case j := <-subscription.NewJob:
				msg <- makeJobStatusUpdate(JobStatusUpdateTypeAdd, j)
			case j := <-subscription.RemovedJob:
				msg <- makeJobStatusUpdate(JobStatusUpdateTypeRemove, j)
			case j := <-subscription.UpdatedJob:
				msg <- makeJobStatusUpdate(JobStatusUpdateTypeUpdate, j)
			case <-ctx.Done():
				close(msg)
				return
			}
		}
	}()

	return msg, nil
}

// JobsLifecycleSubscribe is the correctness-grade half of the split-pipe
// design. It only forwards ADD and REMOVE events, on a dedicated channel
// that is *not* shared with UPDATE traffic. Combined with the large
// `newJob` / `removedJob` upstream buffers (see pkg/job/subscribe.go),
// this guarantees that a client which dutifully drains lifecycle events
// at any reasonable rate will see every job appear and disappear, even
// while a sibling progress subscription is being backpressured.
//
// The goroutine still calls `JobManager.Subscribe` and creates its own
// upstream `updatedJob` channel; we deliberately ignore that channel
// here. The unread channel will fill and `notifyJobUpdate`'s
// non-blocking send will harmlessly drop into the default branch — which
// is exactly the desired behaviour for a client that hasn't asked for
// UPDATE events.
func (r *subscriptionResolver) JobsLifecycleSubscribe(ctx context.Context) (<-chan *JobStatusUpdate, error) {
	// Generous buffer so the resolver goroutine can absorb a transient
	// burst (e.g. starting a generate job that immediately queues many
	// sub-jobs) without blocking the upstream `newJob` channel reader.
	msg := make(chan *JobStatusUpdate, 1000)

	subscription := manager.GetInstance().JobManager.Subscribe(ctx)

	go func() {
		for {
			select {
			case j := <-subscription.NewJob:
				msg <- makeJobStatusUpdate(JobStatusUpdateTypeAdd, j)
			case j := <-subscription.RemovedJob:
				msg <- makeJobStatusUpdate(JobStatusUpdateTypeRemove, j)
			case <-ctx.Done():
				close(msg)
				return
			}
		}
	}()

	return msg, nil
}

// JobsProgressSubscribe is the lossy-by-design half of the split-pipe
// design. Each UPDATE supersedes the previous one for the same job, so
// dropping a tick when the consumer is momentarily slow is the right
// trade-off: the very next tick brings the client back in sync. We use a
// non-blocking inner send so that a slow client can never stall this
// resolver goroutine — and crucially, can never propagate backpressure
// onto a sibling `JobsLifecycleSubscribe`.
//
// The unread `newJob` / `removedJob` channels created by Subscribe will
// fill quickly on a busy server; that's harmless because this client
// hasn't asked for lifecycle events and the JobManager's notifications
// of them are non-blocking by design.
func (r *subscriptionResolver) JobsProgressSubscribe(ctx context.Context) (<-chan *JobStatusUpdate, error) {
	msg := make(chan *JobStatusUpdate, 100)

	subscription := manager.GetInstance().JobManager.Subscribe(ctx)

	go func() {
		for {
			select {
			case j := <-subscription.UpdatedJob:
				select {
				case msg <- makeJobStatusUpdate(JobStatusUpdateTypeUpdate, j):
				default:
					// Outbound buffer full — consumer is slow.
					// Drop this tick; the next one will catch the
					// UI back up to current state.
				}
			case <-ctx.Done():
				close(msg)
				return
			}
		}
	}()

	return msg, nil
}

func (r *subscriptionResolver) ScanCompleteSubscribe(ctx context.Context) (<-chan bool, error) {
	return manager.GetInstance().ScanSubscribe(ctx), nil
}
