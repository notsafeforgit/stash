package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type heldShutdownWorker struct {
	started chan struct{}
	release chan struct{}
}

func (w *heldShutdownWorker) Run(context.Context) error {
	close(w.started)
	<-w.release
	return nil
}

func TestServerShutdownDeadlinePreservesUnfinishedWorker(t *testing.T) {
	w := &heldShutdownWorker{started: make(chan struct{}), release: make(chan struct{})}
	runtime := &backgroundWorkerRuntime{worker: w}
	runtime.start()
	<-w.started
	server := &Server{ingestWorker: runtime}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, server.ShutdownContext(ctx), context.Canceled)
	select {
	case <-runtime.done:
		t.Fatal("unfinished worker was reported as drained")
	default:
	}
	close(w.release)
	deadline, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	require.NoError(t, server.ShutdownContext(deadline))
}

func TestServerShutdownClosesHTTPAtDeadline(t *testing.T) {
	started := make(chan struct{})
	server := &Server{Server: http.Server{Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})}}
	host := httptest.NewUnstartedServer(nil)
	host.Config = &server.Server
	host.Start()
	t.Cleanup(host.Close)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		response, err := host.Client().Get(host.URL)
		if err == nil {
			response.Body.Close()
		}
	}()
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, server.ShutdownContext(ctx), context.DeadlineExceeded)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("HTTP connection survived shutdown deadline")
	}
}
