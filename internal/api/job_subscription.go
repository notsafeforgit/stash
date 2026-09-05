package api

import (
	"context"

	"github.com/stashapp/stash/pkg/job"
)

// Nil inputs disable an event type. Closed inputs are removed from the select
// so a stopped subscription cannot manufacture zero-valued jobs or spin.
func forwardJobUpdates(ctx context.Context, added, removed, updated <-chan job.Job, buffer int, dropUpdates bool) <-chan *JobStatusUpdate {
	output := make(chan *JobStatusUpdate, buffer)
	go func() {
		defer close(output)
		for added != nil || removed != nil || updated != nil {
			var event *JobStatusUpdate
			select {
			case <-ctx.Done():
				return
			case j, ok := <-added:
				if !ok {
					added = nil
					continue
				}
				event = makeJobStatusUpdate(JobStatusUpdateTypeAdd, j)
			case j, ok := <-removed:
				if !ok {
					removed = nil
					continue
				}
				event = makeJobStatusUpdate(JobStatusUpdateTypeRemove, j)
			case j, ok := <-updated:
				if !ok {
					updated = nil
					continue
				}
				event = makeJobStatusUpdate(JobStatusUpdateTypeUpdate, j)
			}

			if dropUpdates && event.Type == JobStatusUpdateTypeUpdate {
				select {
				case <-ctx.Done():
					return
				case output <- event:
				default:
				}
			} else {
				select {
				case <-ctx.Done():
					return
				case output <- event:
				}
			}
		}
	}()
	return output
}
