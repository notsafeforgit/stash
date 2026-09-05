package api

import (
	"context"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/job"
	"github.com/stretchr/testify/require"
)

func TestJobSubscriptionCancelsBlockedSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	added := make(chan job.Job)
	output := forwardJobUpdates(ctx, added, nil, nil, 0, false)
	added <- job.Job{ID: 1}
	cancel()
	// At most the already received event can be delivered before cancellation.
	for {
		select {
		case event, ok := <-output:
			if !ok {
				return
			}
			require.Equal(t, "1", event.Job.ID)
		case <-time.After(time.Second):
			t.Fatal("subscription remained blocked after cancellation")
		}
	}
}

func TestJobSubscriptionDrainsAndClosesInputs(t *testing.T) {
	added, removed, updated := make(chan job.Job, 1), make(chan job.Job, 1), make(chan job.Job, 1)
	added <- job.Job{ID: 1}
	removed <- job.Job{ID: 2}
	updated <- job.Job{ID: 3}
	close(added)
	close(removed)
	close(updated)
	output := forwardJobUpdates(context.Background(), added, removed, updated, 3, false)
	events := map[string]JobStatusUpdateType{}
	for {
		select {
		case event, ok := <-output:
			if !ok {
				require.Equal(t, map[string]JobStatusUpdateType{"1": JobStatusUpdateTypeAdd, "2": JobStatusUpdateTypeRemove, "3": JobStatusUpdateTypeUpdate}, events)
				return
			}
			events[event.Job.ID] = event.Type
		case <-time.After(time.Second):
			t.Fatal("subscription did not close when all inputs closed")
		}
	}
}

func TestJobSubscriptionDropsBackpressuredProgress(t *testing.T) {
	updated := make(chan job.Job)
	output := forwardJobUpdates(context.Background(), nil, nil, updated, 0, true)
	for i := range 10 {
		select {
		case updated <- job.Job{ID: i + 1}:
		case <-time.After(time.Second):
			t.Fatal("progress producer was blocked by the consumer")
		}
	}
	close(updated)
	for range output {
	}
}
