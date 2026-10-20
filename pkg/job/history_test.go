package job

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testHistory struct {
	mu          sync.Mutex
	rows        map[string]Job
	createError error
	failStatus  Status
	cancelError error
	interrupts  int
}

func (h *testHistory) Create(_ context.Context, j Job) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.createError != nil {
		return h.createError
	}
	h.rows[j.ID] = j
	return nil
}
func (h *testHistory) Save(_ context.Context, j Job) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if j.Status == h.failStatus {
		return errors.New("disk full")
	}
	h.rows[j.ID] = j
	return nil
}
func (h *testHistory) Interrupt(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.interrupts++
	return nil
}
func (h *testHistory) Cancel(_ context.Context, id string, _ int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancelError != nil {
		return h.cancelError
	}
	j := h.rows[id]
	j.Status = StatusStopping
	h.rows[id] = j
	return nil
}
func (h *testHistory) Find(_ context.Context, id string) (*Job, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	j, ok := h.rows[id]
	if !ok {
		return nil, nil
	}
	return &j, nil
}
func (h *testHistory) List(context.Context, int, int) ([]Job, error) { return nil, nil }

func TestDurableAdmissionAndOutcomeFailures(t *testing.T) {
	for _, failure := range []string{"admission", "start", "outcome", "none"} {
		t.Run(failure, func(t *testing.T) {
			m := NewManager()
			t.Cleanup(func() { require.NoError(t, m.Shutdown(context.Background())) })
			m.SetSync(true)
			m.RequireHistory()
			executions := 0
			exec := MakeJobExec(func(context.Context, *Progress) error { executions++; return nil })
			id, err := m.Add(t.Context(), "not ready", exec)
			require.ErrorIs(t, err, ErrHistoryUnavailable)
			require.Empty(t, id)
			history := &testHistory{rows: map[string]Job{}}
			switch failure {
			case "admission":
				history.createError = errors.New("disk full")
			case "start":
				history.failStatus = StatusRunning
			case "outcome":
				history.failStatus = StatusFinished
			}
			require.NoError(t, m.EnableHistory(t.Context(), history, nil))
			require.NoError(t, m.EnableHistory(t.Context(), history, nil))
			require.Equal(t, 1, history.interrupts)
			id, err = m.Add(t.Context(), "test", exec)
			if failure == "admission" {
				require.ErrorContains(t, err, "persisting job admission")
				require.Empty(t, id)
				require.Empty(t, m.GetQueue())
				require.Zero(t, executions)
				return
			}
			require.NoError(t, err)
			result, err := m.Find(t.Context(), id)
			require.NoError(t, err)
			persisted, err := history.Find(t.Context(), id)
			require.NoError(t, err)
			require.NotNil(t, result.EndTime)
			require.Equal(t, result.Status, persisted.Status)
			if failure == "none" {
				require.Equal(t, StatusFinished, result.Status)
			} else {
				require.Equal(t, StatusFailed, result.Status)
				require.Contains(t, *result.Error, "persist")
			}
			if failure == "start" {
				require.Zero(t, executions)
			} else {
				require.Equal(t, 1, executions)
			}
		})
	}
}

func TestTimeElapsedUsesFixedCompletion(t *testing.T) {
	start := time.Now().Add(-time.Minute)
	end := start.Add(5 * time.Second)
	require.Equal(t, 5*time.Second, (&Job{StartTime: &start, EndTime: &end}).TimeElapsed())
	require.GreaterOrEqual(t, (&Job{StartTime: &start}).TimeElapsed(), time.Minute)
	require.Zero(t, (&Job{}).TimeElapsed())
}
