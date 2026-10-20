// Package job provides the job execution and management functionality for the application.
package job

import (
	"context"
	"sync"
	"time"
)

type JobExecFn func(ctx context.Context, progress *Progress) error

// JobExec represents the implementation of a Job to be executed.
type JobExec interface {
	Execute(ctx context.Context, progress *Progress) error
}

// PersistedExec admits its command and targets in the same transaction as the
// ordinary job row. Only explicitly restored executors may resume after restart.
type PersistedExec interface {
	JobExec
	Admit(context.Context, Job) error
}

type Restoration struct {
	Job      Job
	Executor PersistedExec
}

type jobExecImpl struct {
	fn JobExecFn
}

func (j *jobExecImpl) Execute(ctx context.Context, progress *Progress) error {
	return j.fn(ctx, progress)
}

// MakeJobExec returns a simple JobExec implementation using the provided
// function.
func MakeJobExec(fn JobExecFn) JobExec {
	return &jobExecImpl{
		fn: fn,
	}
}

// Status is the status of a Job
type Status string

const (
	// StatusReady means that the Job is not yet started.
	StatusReady Status = "READY"
	// StatusRunning means that the job is currently running.
	StatusRunning Status = "RUNNING"
	// StatusStopping means that the job is cancelled but is still running.
	StatusStopping Status = "STOPPING"
	// StatusPaused retains a durable command that needs recovery after an
	// infrastructure failure. It may be cancelled or resumed on server restart.
	StatusPaused Status = "PAUSED"
	// StatusFinished means that the job was completed.
	StatusFinished Status = "FINISHED"
	// StatusCancelled means that the job was cancelled and is now stopped.
	StatusCancelled Status = "CANCELLED"
	// StatusFailed means that the job failed.
	StatusFailed Status = "FAILED"
	// StatusInterrupted means a previous process stopped without a durable outcome.
	StatusInterrupted Status = "INTERRUPTED"
)

// Job represents the status of a queued or running job.
type Job struct {
	ID     string
	Status Status
	// Generation fences status writes from a process superseded by recovery.
	Generation int
	// details of the current operations of the job
	Details     []string
	Description string
	// Progress in terms of 0 - 1.
	Progress  float64
	StartTime *time.Time
	EndTime   *time.Time
	AddTime   time.Time
	Error     *string

	outerCtx   context.Context
	exec       JobExec
	cancelFunc context.CancelFunc
	history    History
	finishing  bool
	resumable  bool
	paused     bool
	persistMu  *sync.Mutex
}

// statusCopy returns a copy of the Job with only the fields needed for
// status reporting. Internal fields (exec, cancelFunc, outerCtx) are
// excluded so that subscription channels don't retain heavy resources.
func (j *Job) statusCopy() Job {
	return Job{
		ID:          j.ID,
		Status:      j.Status,
		Generation:  j.Generation,
		Details:     j.Details,
		Description: j.Description,
		Progress:    j.Progress,
		StartTime:   j.StartTime,
		EndTime:     j.EndTime,
		AddTime:     j.AddTime,
		Error:       j.Error,
	}
}

// TimeElapsed returns the total time elapsed for the job.
// If the EndTime is set, then it uses this to calculate the elapsed time, otherwise it uses time.Now.
func (j *Job) TimeElapsed() time.Duration {
	var end time.Time
	if j.EndTime == nil {
		end = time.Now()
	} else {
		end = *j.EndTime
	}

	if j.StartTime == nil {
		return 0
	}
	return end.Sub(*j.StartTime)
}

func (j *Job) cancel() {
	if j.finishing {
		return
	}
	switch j.Status {
	case StatusReady, StatusPaused:
		j.Status = StatusCancelled
	case StatusRunning:
		j.Status = StatusStopping
	}

	if j.cancelFunc != nil {
		j.cancelFunc()
	}
}

func (j *Job) error(err error) {
	errStr := err.Error()
	j.Error = &errStr
	j.Status = StatusFailed
}

// IsCancelled returns true if cancel has been called on the context.
func IsCancelled(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}
