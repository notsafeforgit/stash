package job

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type persistedTestExec struct {
	history *testHistory
	admit   func()
	run     func(context.Context) error
}

func (e persistedTestExec) Admit(ctx context.Context, j Job) error {
	if e.admit != nil {
		e.admit()
	}
	return e.history.Create(ctx, j)
}
func (e persistedTestExec) Execute(ctx context.Context, _ *Progress) error {
	return e.run(ctx)
}

func TestPersistedShutdownPausesAndExplicitCancellationEndsWork(t *testing.T) {
	for _, cancelExplicitly := range []bool{false, true} {
		t.Run(map[bool]string{false: "shutdown", true: "cancel"}[cancelExplicitly], func(t *testing.T) {
			m := NewManager()
			t.Cleanup(func() { require.NoError(t, m.Shutdown(context.Background())) })
			h := &testHistory{rows: map[string]Job{}}
			require.NoError(t, m.EnableHistory(t.Context(), h, nil))
			started := make(chan struct{})
			first, err := m.Add(t.Context(), "running", persistedTestExec{history: h, run: func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			}})
			require.NoError(t, err)
			<-started
			var executed atomic.Bool
			second, err := m.Add(t.Context(), "queued", persistedTestExec{history: h, run: func(context.Context) error {
				executed.Store(true)
				return nil
			}})
			require.NoError(t, err)
			if cancelExplicitly {
				require.NoError(t, m.CancelJob(second))
				require.NoError(t, m.CancelJob(first))
			}
			require.NoError(t, m.Shutdown(t.Context()))
			require.False(t, executed.Load())
			for _, id := range []string{first, second} {
				row, err := h.Find(t.Context(), id)
				require.NoError(t, err)
				if cancelExplicitly {
					require.Equal(t, StatusCancelled, row.Status)
					require.NotNil(t, row.EndTime)
				} else {
					require.Equal(t, StatusReady, row.Status)
					require.Nil(t, row.EndTime)
				}
			}
		})
	}
}

func TestPersistedCancellationFailureDoesNotClaimCancellation(t *testing.T) {
	m := NewManager()
	t.Cleanup(func() { require.NoError(t, m.Shutdown(context.Background())) })
	h := &testHistory{rows: map[string]Job{}, cancelError: errors.New("disk full")}
	require.NoError(t, m.EnableHistory(t.Context(), h, nil))
	started := make(chan context.Context, 1)
	id, err := m.Add(t.Context(), "running", persistedTestExec{history: h, run: func(ctx context.Context) error {
		started <- ctx
		<-ctx.Done()
		return ctx.Err()
	}})
	require.NoError(t, err)
	ctx := <-started
	require.ErrorContains(t, m.CancelJob(id), "persisting job cancellation")
	require.NoError(t, ctx.Err())
	require.Equal(t, StatusRunning, m.GetJob(id).Status)
	h.mu.Lock()
	h.cancelError = nil
	h.mu.Unlock()
	require.NoError(t, m.CancelJob(id))
	require.Eventually(t, func() bool { return m.GetJob(id).EndTime != nil }, time.Second, time.Millisecond)
	require.Equal(t, StatusCancelled, m.GetJob(id).Status)
}

func TestPersistedAdmissionFinishingDuringShutdownRemainsAccepted(t *testing.T) {
	m := NewManager()
	h := &testHistory{rows: map[string]Job{}}
	require.NoError(t, m.EnableHistory(t.Context(), h, nil))
	admitting, release := make(chan struct{}), make(chan struct{})
	type result struct {
		id  string
		err error
	}
	done := make(chan result, 1)
	var executed atomic.Bool
	go func() {
		id, err := m.Add(t.Context(), "admitting", persistedTestExec{history: h,
			admit: func() { close(admitting); <-release },
			run:   func(context.Context) error { executed.Store(true); return nil },
		})
		done <- result{id, err}
	}()
	<-admitting
	m.Stop()
	close(release)
	accepted := <-done
	require.NoError(t, accepted.err)
	require.NotEmpty(t, accepted.id)
	require.NoError(t, m.Shutdown(t.Context()))
	require.False(t, executed.Load())
	saved, err := h.Find(t.Context(), accepted.id)
	require.NoError(t, err)
	require.Equal(t, StatusReady, saved.Status)
	require.Nil(t, saved.EndTime)
}

func TestPersistedStartFailureKeepsRecoveryState(t *testing.T) {
	m := NewManager()
	t.Cleanup(func() { require.NoError(t, m.Shutdown(context.Background())) })
	m.SetSync(true)
	h := &testHistory{rows: map[string]Job{}, failStatus: StatusRunning}
	require.NoError(t, m.EnableHistory(t.Context(), h, nil))
	var executed bool
	id, err := m.Add(t.Context(), "start failure", persistedTestExec{history: h, run: func(context.Context) error { executed = true; return nil }})
	require.NoError(t, err)
	require.False(t, executed)
	saved, err := h.Find(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, StatusPaused, saved.Status)
	require.Nil(t, saved.EndTime)
	require.Contains(t, *saved.Error, "persisting job start")
}
