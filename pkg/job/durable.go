package job

import (
	"context"
	"encoding/json"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// Durable coordinates restart-safe archive work. The existing Manager remains
// the legacy progress queue until each caller is converted. This service does
// not execute arbitrary arguments or launch a worker on submission.
type Durable struct {
	Repo      models.Repository
	Now       func() time.Time
	MaxActive int
}

func NewDurable(repo models.Repository) *Durable {
	return &Durable{Repo: repo, Now: time.Now, MaxActive: 10000}
}

func (d *Durable) Submit(ctx context.Context, input models.ArchiveJobSubmission) (*models.ArchiveJob, error) {
	var ret *models.ArchiveJob
	err := d.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		ret, err = d.Repo.ArchiveJob.Submit(ctx, input, d.Now(), d.MaxActive)
		return err
	})
	return ret, err
}

func (d *Durable) Claim(ctx context.Context, kind, owner string, duration time.Duration) (*models.ArchiveJob, error) {
	var ret *models.ArchiveJob
	err := d.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		ret, err = d.Repo.ArchiveJob.Claim(ctx, kind, owner, d.Now(), duration)
		return err
	})
	return ret, err
}

func (d *Durable) Renew(ctx context.Context, lease models.ArchiveJobLease, duration time.Duration) (*models.ArchiveJob, error) {
	var ret *models.ArchiveJob
	err := d.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		ret, err = d.Repo.ArchiveJob.Renew(ctx, lease, d.Now(), duration)
		return err
	})
	return ret, err
}

func (d *Durable) Progress(ctx context.Context, lease models.ArchiveJobLease, progress json.RawMessage) (*models.ArchiveJob, error) {
	var ret *models.ArchiveJob
	err := d.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		ret, err = d.Repo.ArchiveJob.Progress(ctx, lease, d.Now(), progress)
		return err
	})
	return ret, err
}

func (d *Durable) Cancel(ctx context.Context, id string, revision int64) (*models.ArchiveJob, error) {
	var ret *models.ArchiveJob
	err := d.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		ret, err = d.Repo.ArchiveJob.Cancel(ctx, id, revision, d.Now())
		return err
	})
	return ret, err
}

func (d *Durable) Recover(ctx context.Context, limit int) (int, error) {
	var ret int
	err := d.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		ret, err = d.Repo.ArchiveJob.Recover(ctx, d.Now(), limit)
		return err
	})
	return ret, err
}

// Checkpoint commits a resumable phase and its progress together without
// completing the job. Progress survives retries and lease recovery. The kind's
// worker owns its complete progress document and preserves prior phase data.
func (d *Durable) Checkpoint(ctx context.Context, lease models.ArchiveJobLease, apply func(context.Context, *models.ArchiveJob) (json.RawMessage, error)) (*models.ArchiveJob, error) {
	var ret *models.ArchiveJob
	err := d.Repo.WithTxn(ctx, func(ctx context.Context) error {
		current, err := d.Repo.ArchiveJob.CheckLease(ctx, lease, d.Now())
		if err != nil {
			return err
		}
		progress, err := apply(ctx, current)
		if err != nil {
			return err
		}
		ret, err = d.Repo.ArchiveJob.Progress(ctx, lease, d.Now(), progress)
		if err != nil {
			return err
		}
		revision := ret.Revision
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
			current, err := d.Repo.ArchiveJob.CheckLease(ctx, lease, d.Now())
			if err != nil {
				return err
			}
			if current.Revision != revision {
				return models.ErrArchiveJobConflict
			}
			return nil
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ret, nil
}

// Publish commits domain writes and the attempt outcome together. Expensive
// hashing/probing happens before this method. A held file descriptor's checks
// can register their own pre-commit hooks in apply. Any failure leaves the job
// running for explicit retry or lease recovery; it never fabricates completion.
func (d *Durable) Publish(ctx context.Context, lease models.ArchiveJobLease, apply func(context.Context, *models.ArchiveJob) (models.ArchiveJobOutcome, error)) (*models.ArchiveJob, error) {
	var ret *models.ArchiveJob
	err := d.Repo.WithTxn(ctx, func(ctx context.Context) error {
		current, err := d.Repo.ArchiveJob.CheckLease(ctx, lease, d.Now())
		if err != nil {
			return err
		}
		outcome, err := apply(ctx, current)
		if err != nil {
			return err
		}
		current, err = d.Repo.ArchiveJob.CheckLease(ctx, lease, d.Now())
		if err != nil {
			return err
		}
		ret, err = d.Repo.ArchiveJob.Finish(ctx, lease, d.Now(), outcome)
		if err != nil {
			return err
		}
		deadline, revision, state := *current.LeaseUntil, ret.Revision, ret.State
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
			if !d.Now().Before(deadline) {
				return models.ErrArchiveJobLease
			}
			stored, err := d.Repo.ArchiveJob.Find(ctx, lease.JobUUID)
			if err != nil {
				return err
			}
			if stored == nil || stored.Revision != revision || stored.Fence != lease.Fence || stored.State != state {
				return models.ErrArchiveJobConflict
			}
			return nil
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ret, nil
}
