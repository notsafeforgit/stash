package translation

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type Provider interface {
	Translate(context.Context, models.TranslationRequest) (models.TranslationCacheInput, error)
}

type ProviderFunc func(context.Context, models.TranslationRequest) (models.TranslationCacheInput, error)

func (f ProviderFunc) Translate(ctx context.Context, request models.TranslationRequest) (models.TranslationCacheInput, error) {
	return f(ctx, request)
}

type Worker struct {
	Service       *Service
	Provider      Provider
	OwnerUUID     string
	LeaseDuration time.Duration
	PollInterval  time.Duration
}

func NewWorker(service *Service, provider Provider) *Worker {
	return &Worker{Service: service, Provider: provider, OwnerUUID: uuid.NewString(), LeaseDuration: time.Minute, PollInterval: time.Second}
}

func (w *Worker) Run(ctx context.Context) error {
	if w.PollInterval <= 0 {
		return models.ErrTranslationWorkInvalid
	}
	for {
		if _, err := w.ProcessNext(ctx); err != nil {
			return err
		}
		// One server worker and a pause between jobs preserve source pacing,
		// including when a large historical backlog is immediately available.
		timer := time.NewTimer(w.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *Worker) ProcessNext(ctx context.Context) (bool, error) {
	if w.Service == nil || w.Service.Durable == nil || w.Provider == nil || !validUUID(w.OwnerUUID) || w.LeaseDuration < 5*time.Second || w.LeaseDuration > 15*time.Minute {
		return false, models.ErrTranslationWorkInvalid
	}
	if _, err := w.Service.Admit(ctx); err != nil && !errors.Is(err, models.ErrArchiveJobCapacity) {
		return false, err
	}
	claimed, err := w.Service.Durable.Claim(ctx, models.ArchiveJobTranslateText, w.OwnerUUID, w.LeaseDuration)
	if err != nil || claimed == nil {
		return false, err
	}
	return true, w.process(ctx, claimed)
}

func (w *Worker) needsProvider(ctx context.Context, work *models.TranslationJobArguments) (bool, error) {
	repo := w.Service.Durable.Repo
	for _, ref := range work.Targets {
		target, err := repo.TranslationWork.Target(ctx, ref.TargetUUID)
		if err != nil {
			return false, err
		}
		if target == nil || target.Revision != ref.Revision || target.State != "pending" || w.Service.Durable.Now().Before(target.NotBefore) {
			continue
		}
		post, err := repo.SourceEvidence.FindPost(ctx, target.PostUUID)
		if err != nil {
			return false, err
		}
		if post == nil {
			return false, models.ErrSourcePayloadCorrupt
		}
		if post.State == "active" {
			return true, nil
		}
	}
	return false, nil
}

func (w *Worker) process(ctx context.Context, claimed *models.ArchiveJob) error {
	durable, repo := w.Service.Durable, w.Service.Durable.Repo
	var work *models.TranslationJobArguments
	err := durable.WithHeartbeat(ctx, claimed.Lease(), w.LeaseDuration, func(ctx context.Context) error {
		var request *models.TranslationRequest
		var cached *models.TranslationCache
		needed := false
		err := repo.WithReadTxn(ctx, func(ctx context.Context) error {
			if _, err := repo.ArchiveJob.CheckLease(ctx, claimed.Lease(), durable.Now()); err != nil {
				return err
			}
			var err error
			_, work, err = w.Service.loadJob(ctx, claimed.UUID)
			if err != nil {
				return err
			}
			request, err = repo.TranslationWork.Request(ctx, work.RequestUUID)
			if err != nil {
				return err
			}
			if request == nil {
				return models.ErrSourcePayloadCorrupt
			}
			cached, err = repo.TranslationWork.Cache(ctx, request.UUID)
			if err != nil || cached != nil {
				return err
			}
			needed, err = w.needsProvider(ctx, work)
			return err
		})
		if err != nil || cached != nil || !needed {
			return err
		}
		result, err := w.Provider.Translate(ctx, *request)
		if err != nil {
			return err
		}
		result.RequestUUID, result.Origin, result.CapturedAt = request.UUID, "worker", durable.Now().UTC().Format(time.RFC3339Nano)
		if _, _, err := archive.PrepareTranslationCache(request, result); err != nil {
			return ErrProviderResponse
		}
		_, err = durable.Checkpoint(ctx, claimed.Lease(), func(ctx context.Context, _ *models.ArchiveJob) (json.RawMessage, error) {
			cached, err := repo.TranslationWork.Cache(ctx, request.UUID)
			if err != nil {
				return nil, err
			}
			if cached == nil {
				cached, err = repo.TranslationWork.RetainCache(ctx, result)
				if err != nil {
					return nil, err
				}
			}
			return json.Marshal(map[string]any{"version": 1, "cache_uuid": cached.UUID})
		})
		return err
	})
	if err != nil {
		return w.fail(ctx, claimed, err)
	}
	if _, err := durable.Renew(ctx, claimed.Lease(), w.LeaseDuration); err != nil {
		return err
	}
	_, err = durable.Publish(ctx, claimed.Lease(), func(ctx context.Context, _ *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		counts := map[string]int{"completed": 0, "review": 0, "superseded": 0}
		for _, ref := range work.Targets {
			target, err := repo.TranslationWork.Target(ctx, ref.TargetUUID)
			if err != nil {
				return models.ArchiveJobOutcome{}, err
			}
			if target == nil || target.Revision != ref.Revision || target.State != "pending" || durable.Now().Before(target.NotBefore) {
				counts["superseded"]++
				continue
			}
			result, err := repo.TranslationWork.PublishTarget(ctx, target.UUID, target.Revision, durable.Now())
			if err != nil {
				return models.ArchiveJobOutcome{}, err
			}
			counts[result.State]++
		}
		result, err := json.Marshal(counts)
		return models.ArchiveJobOutcome{State: "succeeded", Result: result}, err
	})
	if err != nil {
		return w.fail(ctx, claimed, err)
	}
	return nil
}

func (w *Worker) fail(ctx context.Context, claimed *models.ArchiveJob, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(cause, models.ErrArchiveJobLease) || errors.Is(cause, models.ErrArchiveJobConflict) {
		return cause
	}
	code, permanent := "translation_unavailable", false
	switch {
	case errors.Is(cause, ErrProviderInput):
		code, permanent = "translation_input_unsupported", true
	case errors.Is(cause, ErrProviderResponse):
		code = "translation_provider_response"
	case errors.Is(cause, ErrProviderTimeout):
		code = "translation_provider_timeout"
	case errors.Is(cause, models.ErrTranslationWorkInvalid), errors.Is(cause, models.ErrSourcePayloadCorrupt):
		code, permanent = "invalid_translation_work", true
	}
	durable := w.Service.Durable
	_, err := durable.Publish(ctx, claimed.Lease(), func(ctx context.Context, current *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		outcome := models.ArchiveJobOutcome{State: "failed", ErrorCode: code, Result: json.RawMessage(`{}`)}
		if !permanent && current.Fence < int64(current.MaxAttempts) {
			outcome.State = "retry"
			delay := min(24*time.Hour, 5*time.Minute*(1<<min(current.Fence-1, 9)))
			outcome.RetryAt = durable.Now().Add(delay)
		} else if work, err := archive.DecodeTranslationJob(current); err == nil {
			if err := w.Service.hold(ctx, work, durable.Now()); err != nil {
				return models.ArchiveJobOutcome{}, err
			}
		}
		return outcome, nil
	})
	return err
}
