package job

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestShutdownIdleAndRepeated(t *testing.T) {
	m := NewManager()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, m.Shutdown(ctx))
	require.NoError(t, m.Shutdown(ctx))
	m.Stop()
}

func TestShutdownDrainsSerialAndConcurrentJobs(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Stop)
	started, cancelled := make(chan struct{}, 2), make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	executor := MakeJobExec(func(ctx context.Context, _ *Progress) error {
		started <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		<-release
		return ctx.Err()
	})
	serial, admissionErr := m.Add(t.Context(), "serial", executor)
	if admissionErr != nil {
		t.Fatal(admissionErr)
	}
	<-started
	concurrent, admissionErr := m.Start(t.Context(), "concurrent", executor)
	if admissionErr != nil {
		t.Fatal(admissionErr)
	}
	<-started
	neverRun := MakeJobExec(func(context.Context, *Progress) error {
		t.Error("queued work ran after shutdown")
		return nil
	})
	var queued []string
	for range 4 {
		id, err := m.Add(t.Context(), "queued", neverRun)
		require.NoError(t, err)
		queued = append(queued, id)
	}
	m.Stop()
	<-cancelled
	<-cancelled
	for _, id := range queued {
		require.Equal(t, StatusCancelled, m.GetJob(id).Status)
	}
	for _, id := range []string{serial, concurrent} {
		require.Equal(t, StatusStopping, m.GetJob(id).Status)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, m.Shutdown(ctx), context.Canceled, "unfinished executors must hold the drain barrier")
	for _, submit := range []func(context.Context, string, JobExec) (string, error){m.Add, m.Start} {
		id, err := submit(t.Context(), "late admission", neverRun)
		require.ErrorIs(t, err, ErrStopped)
		require.Empty(t, id)
	}

	// Unblock the executors without closing twice in the deferred cleanup.
	release <- struct{}{}
	release <- struct{}{}
	deadline, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	require.NoError(t, m.Shutdown(deadline))
	require.Empty(t, m.GetQueue())
	for _, id := range []string{serial, concurrent} {
		result := m.GetJob(id)
		require.Equal(t, StatusCancelled, result.Status, "cancellation errors must not become failures")
		require.Nil(t, result.Error)
		require.NotNil(t, result.EndTime)
	}
}

func TestConcurrentJobRetiresAndNotifies(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Stop)
	sub := m.Subscribe(t.Context())
	id, admissionErr := m.Start(t.Context(), "concurrent", MakeJobExec(func(context.Context, *Progress) error { return nil }))
	if admissionErr != nil {
		t.Fatal(admissionErr)
	}
	select {
	case result := <-sub.RemovedJob:
		require.Equal(t, id, result.ID)
		require.Equal(t, StatusFinished, result.Status)
	case <-time.After(time.Second):
		t.Fatal("concurrent job did not retire")
	}
	require.Empty(t, m.GetQueue())
}
