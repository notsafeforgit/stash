package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

func (s *TranslationWorkStore) ReadyTargets(ctx context.Context, request string, now time.Time, limit int) ([]models.TranslationTarget, error) {
	if (request != "" && !validSourceRunUUID(request)) || !validJobTime(now) || limit < 1 || limit > archive.MaxTranslationJobTargets {
		return nil, models.ErrTranslationWorkInvalid
	}
	index, scope, args := "translation_targets_ready", "", []any{}
	if request != "" {
		index, scope = "translation_targets_request_ready", "t.request_uuid=? AND "
		args = append(args, request)
	}
	args = append(args, now.UTC(), limit)
	ret := []models.TranslationTarget{}
	// Remove the outer column's TEXT affinity for the JSON-expression lookup;
	// otherwise SQLite scans the active-request index instead of seeking it.
	err := dbWrapper.Select(ctx, &ret, `SELECT t.* FROM translation_targets t INDEXED BY `+index+`
 WHERE `+scope+`t.state='pending' AND t.not_before<=?
 AND NOT EXISTS(SELECT 1 FROM translation_job_targets b WHERE b.target_uuid=t.uuid AND b.target_revision=t.revision)
 AND NOT EXISTS(SELECT 1 FROM archive_jobs j INDEXED BY archive_jobs_translation_request
  WHERE j.kind='text.translate' AND j.state IN ('queued','running') AND json_extract(j.arguments,'$.request_uuid')=+t.request_uuid)
 ORDER BY t.priority DESC,t.not_before,t.uuid LIMIT ?`, args...)
	return ret, err
}

func translationJobSubmissionGuard(ctx context.Context, request string) {
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		current, err := (&ArchiveJobStore{}).FindSubmission(ctx, request)
		if err != nil {
			return err
		}
		work, err := archive.DecodeTranslationJob(current)
		if err != nil {
			return err
		}
		bindings, err := (&TranslationWorkStore{}).JobTargets(ctx, current.UUID)
		if err != nil {
			return err
		}
		if len(bindings) != len(work.Targets) {
			return models.ErrTranslationWorkAtomic
		}
		for i, ref := range work.Targets {
			if bindings[i].TranslationTargetRef != ref {
				return models.ErrTranslationWorkAtomic
			}
		}
		return nil
	})
}

func (s *TranslationWorkStore) TargetBinding(ctx context.Context, target string, revision int) (*models.TranslationJobTarget, error) {
	if !validSourceRunUUID(target) || revision < 1 {
		return nil, models.ErrTranslationWorkInvalid
	}
	ret := &models.TranslationJobTarget{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM translation_job_targets WHERE target_uuid=? AND target_revision=?", target, revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func (s *TranslationWorkStore) JobTargets(ctx context.Context, id string) ([]models.TranslationJobTarget, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrTranslationWorkInvalid
	}
	ret := []models.TranslationJobTarget{}
	err := dbWrapper.Select(ctx, &ret, "SELECT * FROM translation_job_targets WHERE job_uuid=? ORDER BY target_uuid LIMIT ?", id, archive.MaxTranslationJobTargets+1)
	if err == nil && len(ret) > archive.MaxTranslationJobTargets {
		err = models.ErrSourcePayloadCorrupt
	}
	return ret, err
}

// BindJob runs in the submission transaction. Its guard makes swallowing a
// binding error roll the whole transaction back, including the job receipt.
func (s *TranslationWorkStore) BindJob(ctx context.Context, id string, now time.Time) error {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return err
	}
	complete := translationWorkAtomic(ctx)
	if !validJobTime(now) {
		return models.ErrTranslationWorkInvalid
	}
	current, err := (&ArchiveJobStore{}).Find(ctx, id)
	if err != nil {
		return err
	}
	work, err := archive.DecodeTranslationJob(current)
	if err != nil {
		return err
	}
	for _, ref := range work.Targets {
		prior, err := s.TargetBinding(ctx, ref.TargetUUID, ref.Revision)
		if err != nil {
			return err
		}
		if prior != nil {
			if prior.JobUUID != id {
				return models.ErrTranslationWorkConflict
			}
			continue
		}
		target, err := s.Target(ctx, ref.TargetUUID)
		if err != nil {
			return err
		}
		if target == nil || target.RequestUUID != work.RequestUUID || target.Revision != ref.Revision || target.State != "pending" ||
			now.Before(target.NotBefore) || now.Before(target.UpdatedAt) || current.State != "queued" || current.Fence != 0 {
			return models.ErrTranslationWorkConflict
		}
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO translation_job_targets(job_uuid,target_uuid,target_revision) VALUES(?,?,?)", id, ref.TargetUUID, ref.Revision); err != nil {
			return err
		}
	}
	*complete = true
	return nil
}

// RetryTarget explicitly releases held work, or gives an exhausted/cancelled
// target a new revision. The original not-before deadline remains authoritative.
func (s *TranslationWorkStore) RetryTarget(ctx context.Context, id string, expected int, now time.Time) (*models.TranslationTarget, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) {
		return nil, models.ErrTranslationWorkInvalid
	}
	prior, err := s.Target(ctx, id)
	if err != nil {
		return nil, err
	}
	if prior == nil || prior.Revision != expected || now.Before(prior.UpdatedAt) || (prior.State != "held" && prior.State != "pending") {
		return nil, models.ErrTranslationWorkConflict
	}
	if prior.State == "pending" {
		binding, err := s.TargetBinding(ctx, id, expected)
		if err != nil {
			return nil, err
		}
		if binding == nil {
			return nil, models.ErrTranslationWorkConflict
		}
		job, err := (&ArchiveJobStore{}).Find(ctx, binding.JobUUID)
		if err != nil {
			return nil, err
		}
		if job == nil || (job.State != "failed" && job.State != "cancelled") {
			return nil, models.ErrTranslationWorkConflict
		}
	}
	complete := translationWorkAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, "UPDATE translation_targets SET state='pending',revision=revision+1,updated_at=? WHERE uuid=?", now.UTC(), id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Target(ctx, id)
	*complete = err == nil
	return ret, err
}
