package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type EnrichmentWorkStore struct{}

var enrichmentReason = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

const enrichmentTargetSelect = `SELECT t.*,u.url FROM enrichment_targets t JOIN source_post_urls u ON u.uuid=t.url_uuid`

func enrichmentAtomic(ctx context.Context) *bool {
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrEnrichmentAtomic
		}
		return nil
	})
	return &complete
}

func validEnrichmentState(state string) bool {
	return state == "held" || state == "pending" || state == "review" || state == "excluded" || state == "completed"
}

func validEnrichmentSchedule(input models.EnrichmentSchedule) bool {
	return validEnrichmentState(input.State) && input.Priority >= 0 && input.Priority <= 100 && validJobTime(input.NotBefore) &&
		(input.Reason == "" || enrichmentReason.MatchString(input.Reason)) &&
		((input.State != "review" && input.State != "excluded") || input.Reason != "") &&
		(input.State != "completed" || input.Reason == "")
}

func prepareEnrichmentSchedule(input models.EnrichmentSchedule, now time.Time) (models.EnrichmentSchedule, error) {
	if input.NotBefore.IsZero() {
		input.NotBefore = now
	}
	input.NotBefore = input.NotBefore.UTC()
	if !validJobTime(now) || !validEnrichmentSchedule(input) || input.State == "completed" {
		return input, models.ErrEnrichmentInvalid
	}
	return input, nil
}

func validateEnrichmentTarget(value *models.EnrichmentTarget) error {
	id, err := archive.EnrichmentTargetIdentity(value.EnrichmentTargetInput)
	if err != nil || id != value.UUID || !archive.EnrichmentPostURL(value.URL) || value.Revision < 1 ||
		!validEnrichmentSchedule(value.EnrichmentSchedule) || (value.State == "completed") != (value.CompletionUUID != nil) ||
		!validJobTime(value.CreatedAt) || !validJobTime(value.UpdatedAt) || value.UpdatedAt.Before(value.CreatedAt) {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}

func (s *EnrichmentWorkStore) Target(ctx context.Context, id string) (*models.EnrichmentTarget, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrEnrichmentInvalid
	}
	ret := &models.EnrichmentTarget{}
	err := dbWrapper.Get(ctx, ret, enrichmentTargetSelect+" WHERE t.uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ret, validateEnrichmentTarget(ret)
}

func enrichmentEligible(ctx context.Context, input models.EnrichmentTargetInput) error {
	if _, err := activePostLink(ctx, input.PostUUID); err != nil {
		return err
	}
	var eligible bool
	err := dbWrapper.Get(ctx, &eligible, `SELECT EXISTS(SELECT 1 FROM source_collections c
 JOIN source_collection_revisions r ON r.collection_uuid=c.uuid AND r.revision=c.revision
 WHERE c.uuid=? AND c.revision=? AND r.state='active')`, input.CollectionUUID, input.CollectionRevision)
	if err != nil {
		return err
	}
	if !eligible {
		return models.ErrEnrichmentConflict
	}
	return nil
}

func (s *EnrichmentWorkStore) RetainTarget(ctx context.Context, input models.EnrichmentTargetInput, schedule models.EnrichmentSchedule, now time.Time) (*models.EnrichmentTarget, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	id, err := archive.EnrichmentTargetIdentity(input)
	if err != nil {
		return nil, err
	}
	schedule, err = prepareEnrichmentSchedule(schedule, now)
	if err != nil {
		return nil, err
	}
	prior, err := s.Target(ctx, id)
	if err != nil || prior != nil {
		return prior, err
	}
	if _, err := activePostLink(ctx, input.PostUUID); err != nil {
		return nil, err
	}
	var url string
	err = dbWrapper.Get(ctx, &url, "SELECT url FROM source_post_urls WHERE uuid=? AND post_uuid=?", input.URLUUID, input.PostUUID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !archive.EnrichmentPostURL(url)) {
		return nil, models.ErrEnrichmentInvalid
	}
	if err != nil {
		return nil, err
	}
	var exists bool
	if err := dbWrapper.Get(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM source_collection_revisions WHERE collection_uuid=? AND revision=?)", input.CollectionUUID, input.CollectionRevision); err != nil {
		return nil, err
	}
	if !exists {
		return nil, models.ErrEnrichmentInvalid
	}
	if schedule.State == "pending" {
		if err := enrichmentEligible(ctx, input); err != nil {
			return nil, err
		}
	}
	complete := enrichmentAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, `INSERT INTO enrichment_targets(uuid,post_uuid,url_uuid,collection_uuid,collection_revision,policy,origin,state,priority,not_before,reason,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, input.PostUUID, input.URLUUID, input.CollectionUUID, input.CollectionRevision, input.Policy, input.Origin,
		schedule.State, schedule.Priority, schedule.NotBefore, schedule.Reason, now.UTC(), now.UTC())
	if err != nil {
		return nil, err
	}
	ret, err := s.Target(ctx, id)
	*complete = err == nil
	return ret, err
}

func (s *EnrichmentWorkStore) Schedule(ctx context.Context, id string, expected int, schedule models.EnrichmentSchedule, now time.Time) (*models.EnrichmentTarget, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if expected < 1 {
		return nil, models.ErrEnrichmentInvalid
	}
	schedule, err := prepareEnrichmentSchedule(schedule, now)
	if err != nil {
		return nil, err
	}
	prior, err := s.Target(ctx, id)
	if err != nil {
		return nil, err
	}
	if prior == nil || prior.Revision != expected || prior.State == "completed" || now.Before(prior.UpdatedAt) {
		return nil, models.ErrEnrichmentConflict
	}
	if schedule.State == "pending" {
		if err := enrichmentEligible(ctx, prior.EnrichmentTargetInput); err != nil {
			return nil, err
		}
	}
	if prior.State == schedule.State && prior.Priority == schedule.Priority && prior.NotBefore.Equal(schedule.NotBefore) && prior.Reason == schedule.Reason {
		return prior, nil
	}
	complete := enrichmentAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, "UPDATE enrichment_targets SET state=?,priority=?,not_before=?,reason=?,revision=revision+1,updated_at=? WHERE uuid=?",
		schedule.State, schedule.Priority, schedule.NotBefore, schedule.Reason, now.UTC(), id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Target(ctx, id)
	if err == nil {
		err = cancelEnrichmentTargetJob(ctx, id, expected, now)
	}
	*complete = err == nil
	return ret, err
}

func (s *EnrichmentWorkStore) Targets(ctx context.Context, q models.EnrichmentTargetQuery) ([]models.EnrichmentTarget, error) {
	if (q.PostUUID == "") == (q.CollectionUUID == "") || (q.PostUUID != "" && !validSourceRunUUID(q.PostUUID)) ||
		(q.CollectionUUID != "" && !validSourceRunUUID(q.CollectionUUID)) || (q.After != "" && !validSourceRunUUID(q.After)) ||
		(q.State != "" && !validEnrichmentState(q.State)) {
		return nil, models.ErrEnrichmentInvalid
	}
	limit, err := sourcePageLimit(q.Limit)
	if err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	column, id := "post_uuid", q.PostUUID
	if q.CollectionUUID != "" {
		column, id = "collection_uuid", q.CollectionUUID
	}
	where, args := "t."+column+"=? AND t.uuid>?", []any{id, q.After}
	if q.State != "" {
		where += " AND t.state=?"
		args = append(args, q.State)
	}
	args = append(args, limit)
	ret := []models.EnrichmentTarget{}
	err = dbWrapper.Select(ctx, &ret, enrichmentTargetSelect+" WHERE "+where+" ORDER BY t.uuid LIMIT ?", args...)
	return ret, err
}

func (s *EnrichmentWorkStore) History(ctx context.Context, id string, after, limit int) ([]models.EnrichmentTargetHistory, error) {
	if !validSourceRunUUID(id) || after < 0 {
		return nil, models.ErrEnrichmentInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	ret := []models.EnrichmentTargetHistory{}
	err = dbWrapper.Select(ctx, &ret, "SELECT * FROM enrichment_target_history WHERE target_uuid=? AND revision>? ORDER BY revision LIMIT ?", id, after, limit)
	return ret, err
}

const enrichmentReadySources = `
 JOIN source_posts p ON p.uuid=t.post_uuid AND p.state='active'
 JOIN source_collections c ON c.uuid=t.collection_uuid AND c.revision=t.collection_revision
 JOIN source_collection_revisions r ON r.collection_uuid=c.uuid AND r.revision=c.revision AND r.state='active'`

const enrichmentReadyConditions = `t.state='pending' AND t.not_before<=?
 AND (r.root_uuid IS NULL OR EXISTS(SELECT 1 FROM media_roots m JOIN media_root_revisions d ON d.root_uuid=m.uuid AND d.revision=m.revision
  WHERE m.uuid=r.root_uuid AND d.state='active'))
 AND NOT EXISTS(SELECT 1 FROM enrichment_job_targets b WHERE b.target_uuid=t.uuid AND b.target_revision=t.revision)
 AND NOT EXISTS(SELECT 1 FROM archive_jobs j INDEXED BY archive_jobs_enrichment_target
  WHERE j.kind='post.enrich' AND j.state IN ('queued','running') AND json_extract(j.arguments,'$.target_uuid')=+t.uuid)`

const enrichmentReadySelect = enrichmentTargetSelect + enrichmentReadySources + " WHERE t.collection_uuid=? AND " + enrichmentReadyConditions

func (s *EnrichmentWorkStore) Ready(ctx context.Context, collection string, now time.Time, limit int) ([]models.EnrichmentTarget, error) {
	return s.ReadyPage(ctx, collection, now, nil, limit)
}

func (s *EnrichmentWorkStore) ReadyPage(ctx context.Context, collection string, now time.Time, after *models.EnrichmentTargetCursor, limit int) ([]models.EnrichmentTarget, error) {
	if !validSourceRunUUID(collection) || !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	ret := []models.EnrichmentTarget{}
	query, args := enrichmentReadySelect, []any{collection, now.UTC()}
	if after != nil {
		if after.Priority < 0 || after.Priority > 100 || !validJobTime(after.NotBefore) || !validSourceRunUUID(after.UUID) {
			return nil, models.ErrEnrichmentInvalid
		}
		query += " AND (t.priority<? OR (t.priority=? AND (t.not_before>? OR (t.not_before=? AND t.uuid>?))))"
		args = append(args, after.Priority, after.Priority, after.NotBefore.UTC(), after.NotBefore.UTC(), after.UUID)
	}
	query += " ORDER BY t.priority DESC,t.not_before,t.uuid LIMIT ?"
	args = append(args, limit)
	err = dbWrapper.Select(ctx, &ret, query, args...)
	return ret, err
}

type enrichmentCompletionRow struct {
	UUID             string    `db:"uuid"`
	TargetUUID       string    `db:"target_uuid"`
	ExpectedRevision int       `db:"expected_revision"`
	Digest           string    `db:"request_digest"`
	CaptureCount     int       `db:"capture_count"`
	CreatedAt        time.Time `db:"created_at"`
}

func (row enrichmentCompletionRow) resolve(captures []string) (*models.EnrichmentCompletion, error) {
	input := models.EnrichmentCompletionInput{UUID: row.UUID, TargetUUID: row.TargetUUID, ExpectedRevision: row.ExpectedRevision, CaptureUUIDs: captures}
	prepared, err := archive.PrepareEnrichmentCompletion(input)
	if err != nil || len(captures) != row.CaptureCount || !validJobTime(row.CreatedAt) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	digest, err := sourceSignature("stash-enrichment-completion-v1", prepared)
	if err != nil || digest != row.Digest {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.EnrichmentCompletion{EnrichmentCompletionInput: prepared, CreatedAt: row.CreatedAt}, nil
}

func (s *EnrichmentWorkStore) Completion(ctx context.Context, id string) (*models.EnrichmentCompletion, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrEnrichmentInvalid
	}
	var row enrichmentCompletionRow
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM enrichment_completions WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var captures []string
	if err := dbWrapper.Select(ctx, &captures, "SELECT capture_uuid FROM enrichment_completion_captures WHERE completion_uuid=? ORDER BY capture_uuid LIMIT ?", id, archive.MaxEnrichmentCaptures+1); err != nil {
		return nil, err
	}
	return row.resolve(captures)
}

func (s *EnrichmentWorkStore) Complete(ctx context.Context, input models.EnrichmentCompletionInput, now time.Time) (*models.EnrichmentCompletion, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	input, err := archive.PrepareEnrichmentCompletion(input)
	if err != nil {
		return nil, err
	}
	if !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	digest, err := sourceSignature("stash-enrichment-completion-v1", input)
	if err != nil {
		return nil, err
	}
	prior, err := s.Completion(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		original, err := sourceSignature("stash-enrichment-completion-v1", prior.EnrichmentCompletionInput)
		if err != nil {
			return nil, err
		}
		if original != digest {
			return nil, models.ErrEnrichmentConflict
		}
		return prior, nil
	}
	target, err := s.Target(ctx, input.TargetUUID)
	if err != nil {
		return nil, err
	}
	if target == nil || target.Revision != input.ExpectedRevision || target.State != "pending" || now.Before(target.NotBefore) || now.Before(target.UpdatedAt) {
		return nil, models.ErrEnrichmentConflict
	}
	if err := enrichmentEligible(ctx, target.EnrichmentTargetInput); err != nil {
		return nil, err
	}
	query, args, err := sqlx.In(`SELECT count(*) FROM source_captures c JOIN source_collection_captures b ON b.capture_uuid=c.uuid
 WHERE c.post_uuid=? AND b.collection_uuid=? AND b.collection_revision=? AND c.origin IN ('gallery-dl','gallery-dl-enrichment') AND c.uuid IN (?)`,
		target.PostUUID, target.CollectionUUID, target.CollectionRevision, input.CaptureUUIDs)
	if err != nil {
		return nil, err
	}
	var count int
	if err := dbWrapper.Get(ctx, &count, query, args...); err != nil {
		return nil, err
	}
	if count != len(input.CaptureUUIDs) {
		return nil, models.ErrEnrichmentConflict
	}
	complete := enrichmentAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, `INSERT INTO enrichment_completions(uuid,target_uuid,expected_revision,request_digest,capture_count,created_at) VALUES(?,?,?,?,?,?)`,
		input.UUID, input.TargetUUID, input.ExpectedRevision, digest, len(input.CaptureUUIDs), now.UTC())
	if err != nil {
		return nil, err
	}
	for _, capture := range input.CaptureUUIDs {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO enrichment_completion_captures(completion_uuid,capture_uuid) VALUES(?,?)", input.UUID, capture); err != nil {
			return nil, err
		}
	}
	_, err = dbWrapper.Exec(ctx, "UPDATE enrichment_targets SET state='completed',completion_uuid=?,reason='',revision=revision+1,updated_at=? WHERE uuid=?", input.UUID, now.UTC(), target.UUID)
	if err != nil {
		return nil, err
	}
	ret, err := s.Completion(ctx, input.UUID)
	*complete = err == nil
	return ret, err
}
