package job

import (
	"context"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// WithHeartbeat keeps an owned operation alive and cancels it on lost
// ownership. Publication still uses Checkpoint/Publish; renewal alone cannot
// authorize writes. It stops renewal before returning, so callers can finish
// the attempt without a concurrent renewal observing its terminal state.
func (d *Durable) WithHeartbeat(ctx context.Context, lease models.ArchiveJobLease, duration time.Duration, operation func(context.Context) error) error {
	if duration < 5*time.Second || duration > 15*time.Minute || operation == nil {
		return errors.New("invalid archive job heartbeat")
	}
	owned, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	heartbeat, stop := context.WithCancel(owned)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(duration / 3)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeat.Done():
				return
			case <-ticker.C:
				if _, err := d.Renew(heartbeat, lease, duration); err != nil {
					if heartbeat.Err() == nil {
						cancel(err)
					}
					return
				}
			}
		}
	}()
	defer func() { stop(); <-done }()
	err := operation(owned)
	stop()
	<-done
	if owned.Err() != nil {
		return context.Cause(owned)
	}
	return err
}
