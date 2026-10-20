package job

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/logger"
)

var ErrStopped = errors.New("job manager is stopping")
var ErrHistoryUnavailable = errors.New("library job history is not ready")

// ErrResumePending keeps durable checkpoints when infrastructure fails. The
// next opening resumes the accepted command instead of discarding its targets.
var ErrResumePending = errors.New("job requires recovery on the next server start")

const maxGraveyardSize = 10
const defaultThrottleLimit = 100 * time.Millisecond

// Manager maintains a queue of jobs. Jobs are executed one at a time.
type Manager struct {
	queue     []*Job
	graveyard []*Job

	mutex          sync.Mutex
	notEmpty       *sync.Cond
	dispatcherDone chan struct{}
	drained        chan struct{}
	active         sync.WaitGroup
	stopped        bool

	history         History
	historyMu       sync.Mutex
	historyRequired bool

	subscriptions       []*ManagerSubscription
	updateThrottleLimit time.Duration

	isSync bool
}

// NewManager initialises and returns a new Manager.
func NewManager() *Manager {
	ret := &Manager{
		dispatcherDone:      make(chan struct{}),
		drained:             make(chan struct{}),
		updateThrottleLimit: defaultThrottleLimit,
	}

	ret.notEmpty = sync.NewCond(&ret.mutex)

	go ret.dispatcher()

	return ret
}

// Stop is used to stop the dispatcher thread. Once Stop is called, no
// more Jobs will be processed.
func (m *Manager) Stop() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.stopped {
		return
	}
	m.stopped = true
	m.stopAllLocked()
	m.notEmpty.Broadcast()
	go func() {
		m.active.Wait()
		<-m.dispatcherDone
		close(m.drained)
	}()
}

// Shutdown stops admission, pauses durable work, cancels other work and waits for every
// executor (including Start and synchronous jobs) to release its resources.
// A deadline does not relabel an unfinished executor as completed.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.Stop()
	select {
	case <-m.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RequireHistory gates ordinary task admission until the native database is ready.
// Setup/migration jobs use AddTransient because their job table is not yet usable.
func (m *Manager) RequireHistory() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.historyRequired = true
}

// EnableHistory recovers only this process's first opening of the library.
// Repeated setup callbacks must not interrupt work submitted in this process.
func (m *Manager) EnableHistory(ctx context.Context, history History, restore func(context.Context) ([]Restoration, error)) error {
	m.historyMu.Lock()
	defer m.historyMu.Unlock()
	m.mutex.Lock()
	enabled := m.history != nil
	m.mutex.Unlock()
	if enabled {
		return nil
	}
	if err := history.Interrupt(ctx); err != nil {
		return err
	}
	var restored []Restoration
	if restore != nil {
		var err error
		restored, err = restore(ctx)
		if err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, pending := range restored {
		j := pending.Job
		if _, err := uuid.Parse(j.ID); err != nil || j.Status != StatusReady || j.Generation < 1 ||
			j.EndTime != nil || pending.Executor == nil || seen[j.ID] {
			return errors.New("invalid restored library job")
		}
		seen[j.ID] = true
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.stopped {
		return ErrStopped
	}
	// Publish admission and the complete restored queue together. New requests
	// cannot overtake older accepted commands during the recovery window.
	for _, pending := range restored {
		j := pending.Job
		j.exec, j.outerCtx, j.history = pending.Executor, ctx, history
		j.resumable, j.persistMu = true, &sync.Mutex{}
		m.queue = append(m.queue, &j)
		m.notifyNewJob(&j)
	}
	m.history = history
	m.notEmpty.Broadcast()
	return nil
}

// Add persists admission before queuing work. A failed admission never executes.
func (m *Manager) Add(ctx context.Context, description string, e JobExec) (string, error) {
	return m.add(ctx, description, e, false, false)
}

// AddTransient is restricted to initialization work that precedes the native
// database. Its UUID cannot collide with persisted library jobs after setup.
func (m *Manager) AddTransient(ctx context.Context, description string, e JobExec) (string, error) {
	return m.add(ctx, description, e, false, true)
}

// Start persists admission and starts immediately, concurrently with other jobs.
func (m *Manager) Start(ctx context.Context, description string, e JobExec) (string, error) {
	return m.add(ctx, description, e, true, false)
}

func (m *Manager) add(ctx context.Context, description string, e JobExec, immediate, transient bool) (string, error) {
	m.mutex.Lock()
	if m.stopped {
		m.mutex.Unlock()
		return "", ErrStopped
	}
	history := m.history
	if transient {
		history = nil
	}
	if history == nil && m.historyRequired && !transient {
		m.mutex.Unlock()
		return "", ErrHistoryUnavailable
	}
	// Admission participates in shutdown even while persistence is in flight.
	m.active.Add(1)
	m.mutex.Unlock()
	defer m.active.Done()
	persisted, resumable := e.(PersistedExec)
	j := &Job{ID: uuid.NewString(), Status: StatusReady, Description: description, Generation: 1,
		Progress: ProgressIndefinite, AddTime: time.Now(), exec: e, outerCtx: ctx, history: history,
		resumable: resumable, persistMu: &sync.Mutex{}}
	if resumable {
		if history == nil || transient {
			return "", ErrHistoryUnavailable
		}
		if err := persisted.Admit(ctx, j.statusCopy()); err != nil {
			return "", fmt.Errorf("persisting job admission: %w", err)
		}
	} else if history != nil {
		if err := history.Create(ctx, j.statusCopy()); err != nil {
			return "", fmt.Errorf("persisting job admission: %w", err)
		}
	}
	m.mutex.Lock()
	m.queue = append(m.queue, j)
	if m.stopped {
		j.paused = resumable
		j.cancel()
		m.mutex.Unlock()
		m.onJobFinish(j)
		if resumable {
			return j.ID, nil // Durable admission committed before shutdown.
		}
		return "", ErrStopped
	}
	m.notifyNewJob(j)
	synchronous := m.isSync
	var done chan struct{}
	if immediate || synchronous {
		done = m.dispatch(ctx, j)
	} else {
		m.notEmpty.Broadcast()
	}
	m.mutex.Unlock()
	if synchronous {
		<-done
	}
	return j.ID, nil
}

func (m *Manager) notifyNewJob(j *Job) {
	// assumes lock held
	for _, s := range m.subscriptions {
		// don't block if channel is full
		select {
		case s.newJob <- j.statusCopy():
		default:
		}
	}
}

func (m *Manager) getReadyJob() *Job {
	// assumes lock held
	for _, j := range m.queue {
		if j.Status == StatusReady {
			return j
		}
	}

	return nil
}

func (m *Manager) dispatcher() {
	defer close(m.dispatcherDone)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for !m.stopped {
		j := m.getReadyJob()
		if j == nil {
			m.notEmpty.Wait()
			continue
		}
		done := m.dispatch(j.outerCtx, j)
		m.mutex.Unlock()
		<-done
		m.mutex.Lock()
	}
}

func (m *Manager) newProgress(j *Job) *Progress {
	return &Progress{
		updater: &updater{
			m:   m,
			job: j,
		},
		percent: ProgressIndefinite,
	}
}

func (m *Manager) dispatch(ctx context.Context, j *Job) (done chan struct{}) {
	// assumes lock held
	t := time.Now()
	if j.StartTime == nil {
		j.StartTime = &t
	}
	j.Status = StatusRunning

	// create a cancellable context for the job that is not canceled by the outer context
	ctx, cancelFunc := context.WithCancel(context.WithoutCancel(ctx))
	j.cancelFunc = cancelFunc

	done = make(chan struct{})
	m.active.Add(1)
	go m.executeJob(ctx, j, done)

	m.notifyJobUpdate(j)

	return
}

func (m *Manager) executeJob(ctx context.Context, j *Job, done chan struct{}) {
	defer m.active.Done()
	defer close(done)
	defer m.onJobFinish(j)
	defer func() {
		if p := recover(); p != nil {
			// a panic occurred, log and mark the job as failed
			logger.Errorf("panic in job %s - %s: %v", j.ID, j.Description, p)
			logger.Error(string(debug.Stack()))

			m.mutex.Lock()
			defer m.mutex.Unlock()
			if j.Status != StatusStopping {
				j.error(fmt.Errorf("panic: %v", p))
				j.paused = j.resumable
			}
		}
	}()

	if j.history != nil {
		j.persistMu.Lock()
		m.mutex.Lock()
		snapshot := j.statusCopy()
		m.mutex.Unlock()
		err := persistJobStatus(j.history, snapshot)
		j.persistMu.Unlock()
		if err != nil {
			m.mutex.Lock()
			if j.Status != StatusStopping {
				j.error(fmt.Errorf("persisting job start: %w", err))
				j.paused = j.resumable
			}
			m.mutex.Unlock()
			return
		}
	}
	if ctx.Err() != nil {
		return
	}

	progress := m.newProgress(j)
	if err := j.exec.Execute(ctx, progress); err != nil {
		logger.Errorf("task failed due to error: %v", err)
		m.mutex.Lock()
		if j.resumable && errors.Is(err, ErrResumePending) && j.Status != StatusStopping {
			j.paused = true
			j.error(err)
		} else if j.Status != StatusStopping {
			j.error(err)
		}
		m.mutex.Unlock()
	}
}

// Status persistence never holds the queue mutex: jobs may report progress
// while holding a repository transaction, and shutdown must remain responsive.
func persistJobStatus(history History, snapshot Job) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return history.Save(ctx, snapshot)
}

func (m *Manager) onJobFinish(j *Job) {
	j.persistMu.Lock()
	defer j.persistMu.Unlock()
	m.mutex.Lock()
	j.finishing = true
	final := j.statusCopy()
	switch final.Status {
	case StatusStopping, StatusCancelled:
		final.Status = StatusCancelled
	case StatusFailed:
	default:
		final.Status = StatusFinished
	}
	now := time.Now()
	final.EndTime = &now
	if j.paused {
		final.Status, final.EndTime = StatusReady, nil
		if final.Error != nil {
			final.Status = StatusPaused
		}
	}
	m.mutex.Unlock()
	if j.history != nil {
		if err := persistJobOutcome(j.history, final); err != nil {
			// Never announce durable completion without its record. Preserve
			// checkpoints if a resumable outcome could not commit; recovery
			// must still be able to finish notification or remaining edits.
			final.Status = StatusFailed
			final.EndTime = &now
			message := fmt.Sprintf("Job execution ended, but its outcome could not be persisted: %v", err)
			final.Error = &message
			logger.Error(message)
			if j.resumable {
				final.Status = StatusInterrupted
			} else {
				if retryErr := persistJobStatus(j.history, final); retryErr != nil {
					logger.Errorf("persisting failed job outcome: %v", retryErr)
				}
			}
		}
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	j.Status, j.EndTime, j.Error = final.Status, final.EndTime, final.Error
	if j.cancelFunc != nil {
		j.cancelFunc()
		j.cancelFunc = nil
	}
	if final.Status == StatusPaused && !m.stopped {
		// Keep paused work visible and cancellable without retaining the
		// executor. Only startup reconstructs it from its durable command.
		j.finishing = false
		j.exec, j.outerCtx, j.Details = nil, nil, nil
		m.notifyJobUpdate(j)
		return
	}
	m.removeJob(j)
}

func (m *Manager) retireCancelledLocked(j *Job) {
	if j.finishing {
		return
	}
	j.finishing = true
	m.active.Add(1)
	go func() { defer m.active.Done(); m.onJobFinish(j) }()
}

func (m *Manager) removeJob(job *Job) {
	// assumes lock held
	index, _ := m.getJob(m.queue, job.ID)
	if index == -1 {
		return
	}

	// release the executor and subtask details so they can be GC'd
	// while the job remains in the graveyard for status reporting
	job.exec = nil
	job.outerCtx = nil
	job.Details = nil

	m.queue = append(m.queue[:index], m.queue[index+1:]...)

	m.graveyard = append(m.graveyard, job)
	if len(m.graveyard) > maxGraveyardSize {
		m.graveyard = m.graveyard[1:]
	}

	// notify job removed
	for _, s := range m.subscriptions {
		// don't block if channel is full
		select {
		case s.removedJob <- job.statusCopy():
		default:
		}
	}
}

func (m *Manager) getJob(list []*Job, id string) (index int, job *Job) {
	// assumes lock held
	for i, j := range list {
		if j.ID == id {
			index = i
			job = j
			return
		}
	}

	return -1, nil
}

// CancelJob cancels the job with the provided id. Jobs that have been started
// are notified that they are stopping. Jobs that have not yet started are
// removed from the queue. If no job exists with the provided id, then there is
// no effect. Likewise, if the job is already cancelled, there is no effect.
func (m *Manager) CancelJob(id string) error {
	m.mutex.Lock()
	_, j := m.getJob(m.queue, id)
	m.mutex.Unlock()
	if j == nil {
		return nil
	}
	j.persistMu.Lock()
	defer j.persistMu.Unlock()
	m.mutex.Lock()
	finishing, stopped := j.finishing, m.stopped
	m.mutex.Unlock()
	if finishing {
		return nil
	}
	if stopped {
		return ErrStopped
	}
	if j.history != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := j.history.Cancel(ctx, j.ID, j.Generation)
		cancel()
		if err != nil {
			return fmt.Errorf("persisting job cancellation: %w", err)
		}
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	j.paused = false
	j.Error = nil
	j.cancel()
	if j.Status == StatusCancelled {
		m.retireCancelledLocked(j)
	} else {
		m.notifyJobUpdate(j)
	}
	return nil
}

// CancelAll cancels all of the jobs in the queue. This is the same as
// calling CancelJob on all jobs in the queue.
func (m *Manager) CancelAll() error {
	m.mutex.Lock()
	ids := make([]string, 0, len(m.queue))
	for _, j := range m.queue {
		ids = append(ids, j.ID)
	}
	m.mutex.Unlock()
	var failures []error
	for _, id := range ids {
		if err := m.CancelJob(id); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) stopAllLocked() {
	// Walk backwards: removing ready jobs must not skip their neighbours.
	for i := len(m.queue) - 1; i >= 0; i-- {
		j := m.queue[i]
		if j.resumable && (j.Status == StatusReady || j.Status == StatusRunning) {
			j.paused = true
		}
		j.cancel()
		if j.Status == StatusCancelled {
			m.retireCancelledLocked(j)
		} else {
			m.notifyJobUpdate(j)
		}
	}
}

// GetJob returns a copy of the Job for the provided id. Returns nil if the job
// does not exist.
func (m *Manager) GetJob(id string) *Job {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// get from the queue or graveyard
	_, j := m.getJob(append(m.queue, m.graveyard...), id)
	if j != nil {
		jCopy := j.statusCopy()
		return &jCopy
	}

	return nil
}

// Find returns live state or a retained durable outcome after restart.
func (m *Manager) Find(ctx context.Context, id string) (*Job, error) {
	if live := m.GetJob(id); live != nil {
		return live, nil
	}
	m.mutex.Lock()
	history := m.history
	m.mutex.Unlock()
	if history == nil {
		return nil, nil
	}
	return history.Find(ctx, id)
}

func (m *Manager) History(ctx context.Context, limit, offset int) ([]Job, error) {
	m.mutex.Lock()
	history := m.history
	m.mutex.Unlock()
	if history == nil {
		return []Job{}, nil
	}
	return history.List(ctx, limit, offset)
}

// GetQueue returns a copy of the current job queue.
func (m *Manager) GetQueue() []Job {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var ret []Job

	for _, j := range m.queue {
		ret = append(ret, j.statusCopy())
	}

	return ret
}

// Subscribe subscribes to changes to jobs in the manager queue.
func (m *Manager) Subscribe(ctx context.Context) *ManagerSubscription {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	ret := newSubscription()

	m.subscriptions = append(m.subscriptions, ret)

	go func() {
		select {
		case <-ctx.Done():
		case <-m.drained:
		}
		m.mutex.Lock()
		defer m.mutex.Unlock()

		ret.close()

		// remove from the list
		for i, s := range m.subscriptions {
			if s == ret {
				m.subscriptions = append(m.subscriptions[:i], m.subscriptions[i+1:]...)
				break
			}
		}
	}()

	return ret
}

func (m *Manager) notifyJobUpdate(j *Job) {
	// don't update if job is finished or cancelled - these are handled
	// by removeJob
	if j.Status == StatusCancelled || j.Status == StatusFinished || j.Status == StatusFailed {
		return
	}

	// assumes lock held
	for _, s := range m.subscriptions {
		// don't block if channel is full
		select {
		case s.updatedJob <- j.statusCopy():
		default:
		}
	}
}

func (m *Manager) SetSync(isSync bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.isSync = isSync
}

type updater struct {
	m           *Manager
	job         *Job
	lastUpdate  time.Time
	updateTimer *time.Timer
}

func (u *updater) notifyUpdate() {
	// assumes lock held
	u.m.notifyJobUpdate(u.job)
	u.lastUpdate = time.Now()
	u.updateTimer = nil
}

func (u *updater) updateProgress(progress float64, details []string) {
	u.m.mutex.Lock()
	defer u.m.mutex.Unlock()

	u.job.Progress = progress
	u.job.Details = details

	if time.Since(u.lastUpdate) < u.m.updateThrottleLimit {
		if u.updateTimer == nil {
			u.updateTimer = time.AfterFunc(u.m.updateThrottleLimit-time.Since(u.lastUpdate), func() {
				u.m.mutex.Lock()
				defer u.m.mutex.Unlock()

				u.notifyUpdate()
			})
		}
	} else {
		u.notifyUpdate()
	}
}

// A retry of the identical outcome is safe even if the first commit succeeded
// but its acknowledgement was lost. Executors are never retried here.
func persistJobOutcome(history History, final Job) error {
	if err := persistJobStatus(history, final); err != nil {
		return persistJobStatus(history, final)
	}
	return nil
}
