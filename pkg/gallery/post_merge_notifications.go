package gallery

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
)

var (
	ErrPostMergeNotificationInvalid  = errors.New("invalid post merge notification work")
	ErrPostMergeNotificationNotFound = errors.New("post merge notification not found")
)

// PostMergeNotifications only delivers effects from an already committed merge.
// Retrying delivery cannot reapply choices, edit gallery members or merge posts.
type PostMergeNotifications struct{ Durable *job.Durable }

func NewPostMergeNotifications(repo models.Repository) *PostMergeNotifications {
	return &PostMergeNotifications{Durable: job.NewDurable(repo)}
}

func postMergeNotificationReview(ctx context.Context, repo models.Repository, current *models.ArchiveJob) (*models.PostConsolidationReview, error) {
	if current == nil || current.Kind != models.ArchiveJobNotifyPostMerge {
		return nil, ErrPostMergeNotificationNotFound
	}
	work, err := models.ParsePostConsolidationNotification(current.Arguments)
	if err != nil {
		return nil, ErrPostMergeNotificationInvalid
	}
	review, err := repo.SourceEvidence.ConsolidationReview(ctx, work.ReviewUUID)
	if err != nil {
		return nil, err
	}
	if review == nil || !review.Result.Gallery.Changed() || !albumUUID(review.Result.NotificationJobUUID) {
		return nil, ErrPostMergeNotificationInvalid
	}
	original, err := repo.ArchiveJob.Find(ctx, review.Result.NotificationJobUUID)
	if err != nil {
		return nil, err
	}
	if original == nil || original.Kind != current.Kind ||
		original.WorkKey != current.WorkKey || original.ResourceKey != current.ResourceKey || original.MaxAttempts != current.MaxAttempts {
		return nil, ErrPostMergeNotificationInvalid
	}
	if current.UUID == original.UUID {
		if work.ResumeFromJobUUID != "" {
			return nil, ErrPostMergeNotificationInvalid
		}
		return review, nil
	}
	if work.ResumeFromJobUUID == "" || work.ResumeFromJobUUID == current.UUID {
		return nil, ErrPostMergeNotificationInvalid
	}
	parent, err := repo.ArchiveJob.Find(ctx, work.ResumeFromJobUUID)
	if err != nil {
		return nil, err
	}
	if parent == nil || parent.Kind != original.Kind || parent.WorkKey != original.WorkKey || parent.ResourceKey != original.ResourceKey ||
		parent.MaxAttempts != original.MaxAttempts || parent.Sequence >= current.Sequence || parent.Revision != work.ResumeFromJobRevision ||
		(parent.State != "failed" && parent.State != "cancelled") {
		return nil, ErrPostMergeNotificationInvalid
	}
	prior, err := models.ParsePostConsolidationNotification(parent.Arguments)
	if err != nil || prior.ReviewUUID != work.ReviewUUID || (prior.ResumeFromJobUUID == "" && parent.UUID != original.UUID) {
		return nil, ErrPostMergeNotificationInvalid
	}
	return review, nil
}

func (s *PostMergeNotifications) Find(ctx context.Context, id string) (*models.ArchiveJob, error) {
	return s.find(ctx, id, false)
}

func (s *PostMergeNotifications) FindRequest(ctx context.Context, request string) (*models.ArchiveJob, error) {
	return s.find(ctx, request, true)
}

func (s *PostMergeNotifications) find(ctx context.Context, id string, byRequest bool) (*models.ArchiveJob, error) {
	if !albumUUID(id) {
		return nil, ErrPostMergeNotificationInvalid
	}
	var current *models.ArchiveJob
	err := s.Durable.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		if byRequest {
			current, err = s.Durable.Repo.ArchiveJob.FindSubmission(ctx, id)
		} else {
			current, err = s.Durable.Repo.ArchiveJob.Find(ctx, id)
		}
		if err != nil {
			return err
		}
		_, err = postMergeNotificationReview(ctx, s.Durable.Repo, current)
		return err
	})
	return current, err
}

func (s *PostMergeNotifications) Retry(ctx context.Context, request, id string, revision int64) (*models.ArchiveJob, error) {
	if !albumUUID(request) || !albumUUID(id) || revision < 1 {
		return nil, ErrPostMergeNotificationInvalid
	}
	var result *models.ArchiveJob
	repo := s.Durable.Repo
	err := repo.WithTxn(ctx, func(ctx context.Context) error {
		previous, err := repo.ArchiveJob.Find(ctx, id)
		if err != nil {
			return err
		}
		review, err := postMergeNotificationReview(ctx, repo, previous)
		if err != nil {
			return err
		}
		arguments, err := (models.PostConsolidationNotification{Version: 1, ReviewUUID: review.Request.RequestUUID,
			ResumeFromJobUUID: previous.UUID, ResumeFromJobRevision: revision}).Arguments()
		if err != nil {
			return err
		}
		input := models.ArchiveJobSubmission{RequestUUID: request, Kind: previous.Kind, WorkKey: previous.WorkKey, ResourceKey: previous.ResourceKey,
			Arguments: arguments, Priority: previous.Priority, MaxAttempts: previous.MaxAttempts}
		prior, err := repo.ArchiveJob.FindSubmission(ctx, request)
		if err != nil {
			return err
		}
		if prior == nil && (previous.Revision != revision || (previous.State != "failed" && previous.State != "cancelled")) {
			return models.ErrArchiveJobConflict
		}
		result, err = repo.ArchiveJob.Submit(ctx, input, s.Durable.Now(), s.Durable.MaxActive)
		return err
	})
	return result, err
}

func (s *PostMergeNotifications) History(ctx context.Context, id string, before int64, limit int) ([]models.ArchiveJob, error) {
	if !albumUUID(id) || before < 0 || limit < 1 || limit > 100 {
		return nil, ErrPostMergeNotificationInvalid
	}
	result := []models.ArchiveJob{}
	repo := s.Durable.Repo
	err := repo.WithReadTxn(ctx, func(ctx context.Context) error {
		review, err := repo.SourceEvidence.ConsolidationReview(ctx, id)
		if err != nil {
			return err
		}
		if review == nil {
			return ErrPostMergeNotificationNotFound
		}
		if review.Result.NotificationJobUUID == "" {
			return nil
		}
		original, err := repo.ArchiveJob.Find(ctx, review.Result.NotificationJobUUID)
		if err != nil {
			return err
		}
		if original == nil {
			return ErrPostMergeNotificationInvalid
		}
		result, err = repo.ArchiveJob.WorkHistory(ctx, models.ArchiveJobNotifyPostMerge, original.WorkKey, before, limit)
		if err != nil {
			return err
		}
		for i := range result {
			stored, err := postMergeNotificationReview(ctx, repo, &result[i])
			if err != nil {
				return err
			}
			if stored.Request.RequestUUID != id {
				return ErrPostMergeNotificationInvalid
			}
		}
		return nil
	})
	return result, err
}

func (s *PostMergeNotifications) Cancel(ctx context.Context, id string, revision int64) (*models.ArchiveJob, error) {
	if !albumUUID(id) || revision < 1 {
		return nil, ErrPostMergeNotificationInvalid
	}
	var result *models.ArchiveJob
	repo := s.Durable.Repo
	err := repo.WithTxn(ctx, func(ctx context.Context) error {
		current, err := repo.ArchiveJob.Find(ctx, id)
		if err != nil {
			return err
		}
		if _, err := postMergeNotificationReview(ctx, repo, current); err != nil {
			return err
		}
		if current.State == "cancelled" && current.Revision == revision+1 {
			result = current
			return nil
		}
		result, err = repo.ArchiveJob.Cancel(ctx, id, revision, s.Durable.Now())
		return err
	})
	return result, err
}

type PostMergeNotificationWorker struct {
	Service       *PostMergeNotifications
	Effects       func(context.Context, models.PostConsolidationReview, AlbumEffectGuard) error
	OwnerUUID     string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	RetryDelay    time.Duration
}

func NewPostMergeNotificationWorker(service *PostMergeNotifications, effects func(context.Context, models.PostConsolidationReview, AlbumEffectGuard) error) *PostMergeNotificationWorker {
	return &PostMergeNotificationWorker{Service: service, Effects: effects, OwnerUUID: uuid.NewString(), LeaseDuration: time.Minute, PollInterval: time.Second, RetryDelay: 30 * time.Second}
}

func (w *PostMergeNotificationWorker) Run(ctx context.Context) error {
	if w.PollInterval <= 0 {
		return ErrPostMergeNotificationInvalid
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

func (w *PostMergeNotificationWorker) ProcessNext(ctx context.Context) (bool, error) {
	if w.Service == nil || w.Service.Durable == nil || w.Effects == nil || !albumUUID(w.OwnerUUID) ||
		w.LeaseDuration < 5*time.Second || w.LeaseDuration > 15*time.Minute || w.RetryDelay <= 0 {
		return false, ErrPostMergeNotificationInvalid
	}
	durable := w.Service.Durable
	claimed, err := durable.Claim(ctx, models.ArchiveJobNotifyPostMerge, w.OwnerUUID, w.LeaseDuration)
	if err != nil || claimed == nil {
		return false, err
	}
	repo := durable.Repo
	var review *models.PostConsolidationReview
	guard := func(ctx context.Context) error {
		return repo.WithReadTxn(ctx, func(ctx context.Context) error {
			current, err := repo.ArchiveJob.CheckLease(ctx, claimed.Lease(), durable.Now())
			if err != nil {
				return err
			}
			stored, err := postMergeNotificationReview(ctx, repo, current)
			if err != nil {
				return err
			}
			if review != nil && !reflect.DeepEqual(review, stored) {
				return ErrPostMergeNotificationInvalid
			}
			review = stored
			return nil
		})
	}
	err = durable.WithHeartbeat(ctx, claimed.Lease(), w.LeaseDuration, func(ctx context.Context) error {
		if err := guard(ctx); err != nil {
			return err
		}
		return w.Effects(ctx, *review, guard)
	})
	if err != nil {
		return true, w.fail(ctx, claimed, err)
	}
	_, err = durable.Publish(ctx, claimed.Lease(), func(ctx context.Context, current *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		stored, err := postMergeNotificationReview(ctx, repo, current)
		if err != nil {
			return models.ArchiveJobOutcome{}, err
		}
		if !reflect.DeepEqual(review, stored) {
			return models.ArchiveJobOutcome{}, ErrPostMergeNotificationInvalid
		}
		return models.ArchiveJobOutcome{State: "succeeded", Result: json.RawMessage(`{"hooks_finished":true}`)}, nil
	})
	if err != nil {
		return true, w.fail(ctx, claimed, err)
	}
	return true, nil
}

func (w *PostMergeNotificationWorker) fail(ctx context.Context, claimed *models.ArchiveJob, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(cause, models.ErrArchiveJobLease) || errors.Is(cause, models.ErrArchiveJobConflict) {
		return cause
	}
	outcome := models.ArchiveJobOutcome{State: "retry", ErrorCode: "post_merge_notification_unavailable", RetryAt: w.Service.Durable.Now().Add(w.RetryDelay), Result: json.RawMessage(`{"hooks_finished":false}`)}
	if errors.Is(cause, ErrPostMergeNotificationInvalid) || errors.Is(cause, models.ErrSourcePayloadCorrupt) {
		outcome.State, outcome.ErrorCode = "failed", "invalid_post_merge_notification"
	}
	_, err := w.Service.Durable.Publish(ctx, claimed.Lease(), func(context.Context, *models.ArchiveJob) (models.ArchiveJobOutcome, error) { return outcome, nil })
	return err
}
