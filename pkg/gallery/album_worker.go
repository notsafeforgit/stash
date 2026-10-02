package gallery

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

type AlbumEffectGuard func(context.Context) error

// AlbumWorker checkpoints publication before invoking restartable effects.
// Effects must honor cancellation and use the publication's stable event UUID.
type AlbumWorker struct {
	Service       *AlbumBackfill
	Effects       func(context.Context, AlbumPublication, AlbumEffectGuard) error
	OwnerUUID     string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	RetryDelay    time.Duration
}

func NewAlbumWorker(service *AlbumBackfill, effects func(context.Context, AlbumPublication, AlbumEffectGuard) error) *AlbumWorker {
	return &AlbumWorker{Service: service, Effects: effects, OwnerUUID: uuid.NewString(), LeaseDuration: time.Minute, PollInterval: time.Second, RetryDelay: 30 * time.Second}
}

func (w *AlbumWorker) Run(ctx context.Context) error {
	if w.PollInterval <= 0 {
		return ErrAlbumWorkInvalid
	}
	for {
		processed, err := w.ProcessNext(ctx)
		if err != nil {
			return err
		}
		if processed {
			continue
		}
		timer := time.NewTimer(w.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *AlbumWorker) ProcessNext(ctx context.Context) (bool, error) {
	if w.Service == nil || w.Service.Durable == nil || w.Effects == nil || !albumUUID(w.OwnerUUID) || w.LeaseDuration < 5*time.Second || w.LeaseDuration > 15*time.Minute || w.RetryDelay <= 0 {
		return false, ErrAlbumWorkInvalid
	}
	claimed, err := w.Service.Durable.Claim(ctx, models.ArchiveJobBackfillAlbum, w.OwnerUUID, w.LeaseDuration)
	if err != nil || claimed == nil {
		return false, err
	}
	return true, w.process(ctx, claimed)
}

func (w *AlbumWorker) validateResume(ctx context.Context, work *albumWork) error {
	if work.ResumeFromJobUUID == "" {
		return nil
	}
	repo := w.Service.Durable.Repo
	parent, err := repo.ArchiveJob.Find(ctx, work.ResumeFromJobUUID)
	if err != nil {
		return err
	}
	if parent == nil || (parent.State != "failed" && parent.State != "cancelled") {
		return ErrAlbumWorkInvalid
	}
	prior, progress, err := decodeAlbumWork(parent)
	if err != nil {
		return err
	}
	if prior.PostUUID != work.PostUUID || prior.Policy != work.Policy || prior.Signature != work.Signature || !reflect.DeepEqual(progress.Publication, work.ResumePublication) {
		return ErrAlbumWorkInvalid
	}
	if work.ResumePublication != nil {
		// Check the actual publication checkpoint, not an arbitrarily long
		// chain of retry descriptions or a fabricated resume argument.
		origin, err := repo.ArchiveJob.Find(ctx, work.ResumePublication.EventUUID)
		if err != nil {
			return err
		}
		original, committed, err := decodeAlbumWork(origin)
		if err != nil {
			return err
		}
		if original.ResumePublication != nil || committed.Version != 1 || original.PostUUID != work.PostUUID || original.Policy != work.Policy || original.Signature != work.Signature ||
			!reflect.DeepEqual(committed.Publication, work.ResumePublication) {
			return ErrAlbumWorkInvalid
		}
	}
	return nil
}

func albumPublication(event, post string, result *models.SourceAlbumBackfillResult) *AlbumPublication {
	return &AlbumPublication{EventUUID: event, PostUUID: post, GalleryUUID: result.Gallery.GalleryUUID, Action: result.Gallery.Action, Created: result.Gallery.Created,
		Selected: result.Selected, Review: result.Review, Unavailable: result.Unavailable, Added: len(result.Gallery.Added), Removed: len(result.Gallery.Removed)}
}

func (w *AlbumWorker) process(ctx context.Context, claimed *models.ArchiveJob) error {
	work, progress, err := decodeAlbumWork(claimed)
	if err != nil {
		return w.fail(ctx, claimed, err)
	}
	durable, repo := w.Service.Durable, w.Service.Durable.Repo
	err = durable.WithHeartbeat(ctx, claimed.Lease(), w.LeaseDuration, func(ctx context.Context) error {
		_, err := durable.Checkpoint(ctx, claimed.Lease(), func(ctx context.Context, current *models.ArchiveJob) (json.RawMessage, error) {
			if err := w.validateResume(ctx, work); err != nil {
				return nil, err
			}
			_, currentProgress, err := decodeAlbumWork(current)
			if err != nil {
				return nil, err
			}
			progress = currentProgress
			if progress.Publication == nil {
				result, err := repo.SourceGallery.Backfill(ctx, work.PostUUID, work.Policy, work.Signature)
				if err != nil {
					return nil, err
				}
				progress.Publication = albumPublication(claimed.UUID, work.PostUUID, result)
			}
			progress.Version = 1
			return json.Marshal(progress)
		})
		if err != nil {
			return err
		}
		guard := func(ctx context.Context) error {
			return repo.WithReadTxn(ctx, func(ctx context.Context) error {
				current, err := repo.ArchiveJob.CheckLease(ctx, claimed.Lease(), durable.Now())
				if err != nil {
					return err
				}
				_, stored, err := decodeAlbumWork(current)
				if err != nil {
					return err
				}
				if stored.Version != 1 || !reflect.DeepEqual(stored.Publication, progress.Publication) {
					return ErrAlbumWorkInvalid
				}
				return nil
			})
		}
		if err := guard(ctx); err != nil {
			return err
		}
		return w.Effects(ctx, *progress.Publication, guard)
	})
	if err != nil {
		return w.fail(ctx, claimed, err)
	}
	if _, err := durable.Renew(ctx, claimed.Lease(), w.LeaseDuration); err != nil {
		return err
	}
	_, err = durable.Publish(ctx, claimed.Lease(), func(_ context.Context, current *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		_, stored, err := decodeAlbumWork(current)
		if err != nil {
			return models.ArchiveJobOutcome{}, err
		}
		if stored.Version != 1 || !reflect.DeepEqual(stored.Publication, progress.Publication) {
			return models.ArchiveJobOutcome{}, ErrAlbumWorkInvalid
		}
		return models.ArchiveJobOutcome{State: "succeeded", Result: json.RawMessage(`{"hooks_finished":true}`)}, nil
	})
	if err != nil {
		return w.fail(ctx, claimed, err)
	}
	return nil
}

func (w *AlbumWorker) fail(ctx context.Context, claimed *models.ArchiveJob, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(cause, models.ErrArchiveJobLease) || errors.Is(cause, models.ErrArchiveJobConflict) {
		return cause
	}
	code, permanent := "album_processing_unavailable", false
	switch {
	case errors.Is(cause, models.ErrSourceGalleryConflict), errors.Is(cause, models.ErrSourceAttachmentConflict):
		code, permanent = "album_preview_changed", true
	case errors.Is(cause, models.ErrSourcePostForgotten):
		code, permanent = "album_post_forgotten", true
	case errors.Is(cause, models.ErrSourceAlbumLimit):
		code, permanent = "album_review_limit", true
	case errors.Is(cause, ErrAlbumWorkInvalid), errors.Is(cause, ErrAlbumWorkNotFound), errors.Is(cause, models.ErrSourceAlbumPolicy), errors.Is(cause, models.ErrSourcePayloadCorrupt):
		code, permanent = "invalid_album_work", true
	}
	durable := w.Service.Durable
	outcome := models.ArchiveJobOutcome{State: "failed", ErrorCode: code, Result: json.RawMessage(`{"hooks_finished":false}`)}
	if !permanent {
		outcome.State, outcome.RetryAt = "retry", durable.Now().Add(w.RetryDelay)
	}
	_, err := durable.Publish(ctx, claimed.Lease(), func(context.Context, *models.ArchiveJob) (models.ArchiveJobOutcome, error) { return outcome, nil })
	return err
}
