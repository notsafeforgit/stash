package translation

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
)

var ErrNotFound = errors.New("translation work not found")

type Service struct {
	Durable   *job.Durable
	MaxActive int
}

func New(repo models.Repository) *Service {
	return &Service{Durable: job.NewDurable(repo), MaxActive: 64}
}

func validUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

// Admit freezes at most fifty due targets of one request in one transaction.
// The translation-specific ceiling leaves room for media and album work in
// the durable queue. Admission never invokes the provider.
func (s *Service) Admit(ctx context.Context) (*models.ArchiveJob, error) {
	if s.Durable == nil || s.MaxActive < 1 || s.MaxActive > 100 {
		return nil, models.ErrTranslationWorkInvalid
	}
	repo := s.Durable.Repo
	var ret *models.ArchiveJob
	err := repo.WithTxn(ctx, func(ctx context.Context) error {
		active := 0
		for _, state := range []string{"queued", "running"} {
			rows, err := repo.ArchiveJob.List(ctx, models.ArchiveJobTranslateText, state, 0, s.MaxActive)
			if err != nil {
				return err
			}
			active += len(rows)
		}
		if active >= s.MaxActive {
			return nil
		}
		now := s.Durable.Now()
		first, err := repo.TranslationWork.ReadyTargets(ctx, "", now, 1)
		if err != nil || len(first) == 0 {
			return err
		}
		targets, err := repo.TranslationWork.ReadyTargets(ctx, first[0].RequestUUID, now, archive.MaxTranslationJobTargets)
		if err != nil {
			return err
		}
		work := models.TranslationJobArguments{Version: 1, RequestUUID: first[0].RequestUUID}
		priority := 0
		for _, target := range targets {
			work.Targets = append(work.Targets, models.TranslationTargetRef{TargetUUID: target.UUID, Revision: target.Revision})
			priority = max(priority, target.Priority)
		}
		slices.SortFunc(work.Targets, func(a, b models.TranslationTargetRef) int { return strings.Compare(a.TargetUUID, b.TargetUUID) })
		submission, err := archive.PrepareTranslationJob(work)
		if err != nil {
			return err
		}
		submission.Priority = priority
		ret, err = repo.ArchiveJob.Submit(ctx, submission, now, s.Durable.MaxActive)
		if err != nil {
			return err
		}
		return repo.TranslationWork.BindJob(ctx, ret.UUID, now)
	})
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (s *Service) loadJob(ctx context.Context, id string) (*models.ArchiveJob, *models.TranslationJobArguments, error) {
	if !validUUID(id) {
		return nil, nil, models.ErrTranslationWorkInvalid
	}
	repo := s.Durable.Repo
	current, err := repo.ArchiveJob.Find(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if current == nil || current.Kind != models.ArchiveJobTranslateText {
		return nil, nil, ErrNotFound
	}
	work, err := archive.DecodeTranslationJob(current)
	if err != nil {
		return nil, nil, err
	}
	bindings, err := repo.TranslationWork.JobTargets(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if len(bindings) != len(work.Targets) {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	for i, ref := range work.Targets {
		if bindings[i].TranslationTargetRef != ref {
			return nil, nil, models.ErrSourcePayloadCorrupt
		}
	}
	return current, work, nil
}

func (s *Service) Status(ctx context.Context, id string) (*models.ArchiveJob, error) {
	var ret *models.ArchiveJob
	err := s.Durable.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		ret, _, err = s.loadJob(ctx, id)
		return err
	})
	return ret, err
}

func (s *Service) Cancel(ctx context.Context, id string, expected int64) (*models.ArchiveJob, error) {
	var ret *models.ArchiveJob
	err := s.Durable.Repo.WithTxn(ctx, func(ctx context.Context) error {
		_, work, err := s.loadJob(ctx, id)
		if err != nil {
			return err
		}
		now := s.Durable.Now()
		ret, err = s.Durable.Repo.ArchiveJob.Cancel(ctx, id, expected, now)
		if err != nil {
			return err
		}
		return s.hold(ctx, work, now)
	})
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (s *Service) hold(ctx context.Context, work *models.TranslationJobArguments, now time.Time) error {
	for _, ref := range work.Targets {
		target, err := s.Durable.Repo.TranslationWork.Target(ctx, ref.TargetUUID)
		if err != nil {
			return err
		}
		if target == nil || target.State != "pending" || target.Revision != ref.Revision {
			continue
		}
		_, err = s.Durable.Repo.TranslationWork.ScheduleTarget(ctx, target.UUID, target.Revision,
			models.TranslationTargetSchedule{State: "held", Priority: target.Priority, NotBefore: target.NotBefore}, now)
		if err != nil {
			return err
		}
	}
	return nil
}
