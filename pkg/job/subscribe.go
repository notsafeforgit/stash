package job

// ManagerSubscription is a collection of channels that will receive updates
// from the job manager.
type ManagerSubscription struct {
	// new jobs are sent to this channel
	NewJob <-chan Job
	// removed jobs are sent to this channel
	RemovedJob <-chan Job
	// updated jobs are sent to this channel
	UpdatedJob <-chan Job

	newJob     chan Job
	removedJob chan Job
	updatedJob chan Job
}

// Buffer sizing rationale:
//
//   - newJob / removedJob carry lifecycle events. They're rare (one per
//     queued or completed job — typically dozens per hour, not thousands)
//     but each one is correctness-grade: if a UI drops an ADD it never
//     learns the job exists. We give these a large buffer so notifyNewJob
//     / notifyJobRemoved (which use non-blocking sends to avoid stalling
//     mutations) effectively never drop in practice.
//   - updatedJob carries progress ticks — frequent (potentially hundreds
//     per second during scans / generates) and superseded by the next
//     tick, so dropping under backpressure is the correct behaviour, not
//     a bug. A modest buffer is fine.
//
// The GraphQL subscription layer (resolver_subscription_job.go) splits
// lifecycle and progress into two independent end-to-end pipelines, so a
// UPDATE storm filling a client's progress buffer cannot starve its
// lifecycle buffer of consumer attention.
func newSubscription() *ManagerSubscription {
	ret := &ManagerSubscription{
		newJob:     make(chan Job, 10000),
		removedJob: make(chan Job, 10000),
		updatedJob: make(chan Job, 100),
	}

	ret.NewJob = ret.newJob
	ret.RemovedJob = ret.removedJob
	ret.UpdatedJob = ret.updatedJob

	return ret
}

func (s *ManagerSubscription) close() {
	close(s.newJob)
	close(s.removedJob)
	close(s.updatedJob)
}
