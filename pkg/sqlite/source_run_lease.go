package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func (s *SourceRunStore) Claim(ctx context.Context, id, producer, owner, policy string, now time.Time, duration time.Duration) (*models.SourceRun, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || !validSourceRunUUID(producer) || !validSourceRunUUID(owner) || !validJobLease(now, duration) {
		return nil, models.ErrSourceRunInvalid
	}
	if _, err := s.Recover(ctx, now, 100); err != nil {
		return nil, err
	}
	row, err := findSourceRun(ctx, "SELECT * FROM source_runs WHERE uuid=?", id)
	if err != nil || row == nil {
		return nil, err
	}
	if row.Policy != policy {
		return nil, models.ErrSourceRunConflict
	}
	if row.State == "running" && row.Producer.String == producer && row.Owner.String == owner && row.LeaseUntil.Int64 > now.UnixMilli() {
		r, err := row.resolve(ctx)
		if err != nil {
			return nil, err
		}
		if _, _, err := sourceRunDefinition(ctx, r); err != nil {
			return nil, err
		}
		return r, nil // a lost claim response does not create a second attempt
	}
	if row.State != "queued" || row.Available > now.UnixMilli() {
		return nil, nil
	}
	r, err := row.resolve(ctx)
	if err != nil {
		return nil, err
	}
	collection, root, err := sourceRunDefinition(ctx, r)
	if errors.Is(err, models.ErrSourceDefinitionConflict) {
		_, err = dbWrapper.Exec(ctx, "UPDATE source_runs SET state='deferred',error_code='definition_changed',revision=revision+1,updated_at_ms=? WHERE uuid=?", now.UnixMilli(), id)
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	destination, prefix, err := scrape.Destination(root, collection.PathPrefix)
	if err != nil {
		return nil, err
	}
	rootIdentity := ""
	if root != nil {
		rootIdentity = root.Binding.DirectoryIdentity
	}
	var busy bool
	if err := dbWrapper.Get(ctx, &busy, `SELECT EXISTS(SELECT 1 FROM source_runs WHERE state='running' AND (collection_uuid=? OR target_key=?))
OR EXISTS(SELECT 1 FROM source_run_cooldowns WHERE target_key=? AND available_at_ms>?)`, collection.UUID, row.TargetKey, row.TargetKey, now.UnixMilli()); err != nil {
		return nil, err
	}
	if busy {
		return nil, nil
	}
	if r.Operation == "enrich" {
		if err := dbWrapper.Get(ctx, &busy, "SELECT EXISTS(SELECT 1 FROM source_runs WHERE collection_uuid=? AND operation='download' AND state='queued' AND available_at_ms<=?)", collection.UUID, now.UnixMilli()); err != nil {
			return nil, err
		}
		if busy {
			return nil, nil
		}
	}
	if destination != "" {
		// Boundary-aware comparisons keep Foo separate from Foobar. Root
		// identity also handles aliases of the same host/container mount.
		if err := dbWrapper.Get(ctx, &busy, `SELECT EXISTS(SELECT 1 FROM source_runs WHERE state='running' AND destination!='' AND
 (destination=? OR substr(destination,1,length(?))=? OR substr(?,1,length(rtrim(destination,'/')||'/'))=rtrim(destination,'/')||'/'
 OR (root_identity=? AND (destination_prefix=? OR destination_prefix='.' OR ?='.'
 OR substr(destination_prefix,1,length(?))=? OR substr(?,1,length(destination_prefix||'/'))=destination_prefix||'/'))))`,
			destination, strings.TrimRight(destination, "/")+"/", strings.TrimRight(destination, "/")+"/", destination,
			rootIdentity, prefix, prefix, prefix+"/", prefix+"/", prefix); err != nil {
			return nil, err
		}
		if busy {
			return nil, nil
		}
	}
	// Latest uncovered range first; old history remains durable pending work.
	window := r.Pending[len(r.Pending)-1]
	r.Pending = r.Pending[:len(r.Pending)-1]
	pendingJSON, err := json.Marshal(r.Pending)
	if err != nil {
		return nil, err
	}
	windowJSON, err := json.Marshal(window)
	if err != nil {
		return nil, err
	}
	progressJSON, err := json.Marshal(models.SourceRunProgress{})
	if err != nil {
		return nil, err
	}
	if r.Fence > 0 {
		// A checkpoint is meaningful only for the same exact traversal window.
		// Widening an interrupted window restarts that traversal deliberately.
		var resumable bool
		if err := dbWrapper.Get(ctx, &resumable, "SELECT EXISTS(SELECT 1 FROM source_run_attempts WHERE run_uuid=? AND fence=? AND window=? AND outcome IN ('retry','expired','deferred'))", id, r.Fence, string(windowJSON)); err != nil {
			return nil, err
		}
		if resumable {
			progressJSON = []byte(row.Progress)
		}
	}
	_, err = dbWrapper.Exec(ctx, `UPDATE source_runs SET state='running',fence=fence+1,revision=revision+1,producer_uuid=?,owner_uuid=?,lease_until_ms=?,
pending=?,window=?,progress=?,destination=?,root_identity=?,destination_prefix=?,error_code='',updated_at_ms=? WHERE uuid=?`,
		producer, owner, now.Add(duration).UnixMilli(), string(pendingJSON), string(windowJSON), string(progressJSON), destination, rootIdentity, prefix, now.UnixMilli(), id)
	if err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_run_attempts(run_uuid,fence,producer_uuid,owner_uuid,window,progress,started_at_ms)
SELECT uuid,fence,producer_uuid,owner_uuid,window,progress,? FROM source_runs WHERE uuid=?`, now.UnixMilli(), id)
	if err != nil {
		return nil, err
	}
	return s.Find(ctx, id)
}

func (s *SourceRunStore) CheckLease(ctx context.Context, lease models.SourceRunLease, now time.Time) (*models.SourceRun, error) {
	if !validSourceRunUUID(lease.RunUUID) || !validSourceRunUUID(lease.ProducerUUID) || !validSourceRunUUID(lease.OwnerUUID) || lease.Fence < 1 || !validJobTime(now) {
		return nil, models.ErrSourceRunInvalid
	}
	r, err := s.Find(ctx, lease.RunUUID)
	if err != nil {
		return nil, err
	}
	if r == nil || r.State != "running" || r.LeaseUntil == nil || !now.Before(*r.LeaseUntil) || r.Lease() != lease {
		return nil, models.ErrSourceRunLease
	}
	return r, nil
}

func (s *SourceRunStore) Renew(ctx context.Context, lease models.SourceRunLease, now time.Time, duration time.Duration) (*models.SourceRun, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobLease(now, duration) {
		return nil, models.ErrSourceRunInvalid
	}
	r, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	if _, _, err := sourceRunDefinition(ctx, r); err != nil {
		return nil, err
	}
	deadline := now.Add(duration)
	if deadline.Before(*r.LeaseUntil) {
		deadline = *r.LeaseUntil
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE source_runs SET lease_until_ms=?,revision=revision+1,updated_at_ms=? WHERE uuid=?", deadline.UnixMilli(), now.UnixMilli(), r.UUID); err != nil {
		return nil, err
	}
	return s.Find(ctx, r.UUID)
}

func (s *SourceRunStore) Progress(ctx context.Context, lease models.SourceRunLease, p models.SourceRunProgress, now time.Time) (*models.SourceRun, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if p.ItemsSeen < 0 || p.FilesCompleted < 0 || len(p.Cursor) > 1024 || strings.ContainsFunc(p.Cursor, unicode.IsControl) {
		return nil, models.ErrSourceRunInvalid
	}
	r, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	if _, _, err := sourceRunDefinition(ctx, r); err != nil {
		return nil, err
	}
	if p.ItemsSeen < r.Progress.ItemsSeen || p.FilesCompleted < r.Progress.FilesCompleted {
		return nil, models.ErrSourceRunConflict
	}
	if p.ItemsSeen == r.Progress.ItemsSeen && p.Cursor != r.Progress.Cursor {
		return nil, models.ErrSourceRunConflict
	}
	if p == r.Progress {
		return r, nil
	}
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE source_runs SET progress=?,revision=revision+1,updated_at_ms=? WHERE uuid=?", string(body), now.UnixMilli(), r.UUID); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE source_run_attempts SET progress=? WHERE run_uuid=? AND fence=?", string(body), r.UUID, r.Fence); err != nil {
		return nil, err
	}
	return s.Find(ctx, r.UUID)
}

func (s *SourceRunStore) Finish(ctx context.Context, lease models.SourceRunLease, outcome models.SourceRunOutcome, now time.Time) (*models.SourceRun, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if (outcome.State != "succeeded" && outcome.State != "retry" && outcome.State != "deferred") || !jobErrorCode(outcome.ErrorCode) || outcome.RetryAfterSeconds < 0 || outcome.RetryAfterSeconds > 604800 ||
		(outcome.State == "succeeded" && (outcome.ErrorCode != "" || outcome.RetryAfterSeconds != 0)) || (outcome.State != "succeeded" && outcome.ErrorCode == "") {
		return nil, models.ErrSourceRunInvalid
	}
	r, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	if outcome.State == "succeeded" {
		if _, _, err := sourceRunDefinition(ctx, r); err != nil {
			return nil, err
		}
	}
	return s.finish(ctx, r, outcome, now)
}

func (s *SourceRunStore) finish(ctx context.Context, r *models.SourceRun, outcome models.SourceRunOutcome, now time.Time) (*models.SourceRun, error) {
	state := "queued"
	delay := time.Duration(r.CooldownSeconds) * time.Second
	if outcome.State == "succeeded" {
		r.Completed = scrape.Union(r.Completed, []models.SourceWindow{*r.Window})
		r.Failures = 0
		if len(r.Pending) == 0 {
			state = "succeeded"
		}
	} else {
		r.Pending = scrape.Union(r.Pending, []models.SourceWindow{*r.Window})
		r.Failures++
		if outcome.State == "deferred" || r.Failures >= 8 {
			state = "deferred"
		}
		backoff := min(24*time.Hour, 5*time.Minute*time.Duration(1<<min(r.Failures-1, 8)))
		delay = max(delay, backoff, time.Duration(outcome.RetryAfterSeconds)*time.Second)
	}
	pending, err := json.Marshal(r.Pending)
	if err != nil {
		return nil, err
	}
	completed, err := json.Marshal(r.Completed)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE source_run_attempts SET outcome=?,error_code=?,ended_at_ms=? WHERE run_uuid=? AND fence=?", outcome.State, outcome.ErrorCode, now.UnixMilli(), r.UUID, r.Fence); err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `UPDATE source_runs SET state=?,pending=?,completed=?,failures=?,available_at_ms=?,error_code=?,window=NULL,producer_uuid=NULL,owner_uuid=NULL,lease_until_ms=NULL,revision=revision+1,updated_at_ms=? WHERE uuid=?`,
		state, string(pending), string(completed), r.Failures, now.Add(delay).UnixMilli(), outcome.ErrorCode, now.UnixMilli(), r.UUID)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_run_cooldowns(target_key,available_at_ms) SELECT target_key,? FROM source_runs WHERE uuid=?
ON CONFLICT(target_key) DO UPDATE SET available_at_ms=max(available_at_ms,excluded.available_at_ms)`, now.Add(delay).UnixMilli(), r.UUID); err != nil {
		return nil, err
	}
	return s.Find(ctx, r.UUID)
}

func (s *SourceRunStore) Recover(ctx context.Context, now time.Time, limit int) (int, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return 0, err
	}
	if !validJobTime(now) || limit < 1 || limit > 100 {
		return 0, models.ErrSourceRunInvalid
	}
	var rows []sourceRunRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM source_runs WHERE state='running' AND lease_until_ms<=? ORDER BY lease_until_ms,id LIMIT ?", now.UnixMilli(), limit); err != nil {
		return 0, err
	}
	for _, row := range rows {
		r, err := row.resolve(ctx)
		if err != nil {
			return 0, err
		}
		if _, err := s.finish(ctx, r, models.SourceRunOutcome{State: "expired", ErrorCode: "lease_expired"}, now); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

func (s *SourceRunStore) Review(ctx context.Context, id string, revision int64, action string, now time.Time) (*models.SourceRun, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || revision < 1 || !validJobTime(now) || (action != "retry" && action != "cancel") {
		return nil, models.ErrSourceRunInvalid
	}
	r, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if r == nil || r.Revision != revision || r.State == "succeeded" || r.State == "cancelled" {
		return nil, models.ErrSourceRunConflict
	}
	if action == "retry" {
		if r.State != "deferred" {
			return nil, models.ErrSourceRunConflict
		}
		if _, _, err := sourceRunDefinition(ctx, r); err != nil {
			return nil, err
		}
		_, err = dbWrapper.Exec(ctx, "UPDATE source_runs SET state='queued',failures=0,error_code='',revision=revision+1,updated_at_ms=? WHERE uuid=?", now.UnixMilli(), id)
	} else {
		if r.Window != nil {
			r.Pending = scrape.Union(r.Pending, []models.SourceWindow{*r.Window})
			if _, err := dbWrapper.Exec(ctx, "UPDATE source_run_attempts SET outcome='cancelled',ended_at_ms=? WHERE run_uuid=? AND fence=?", now.UnixMilli(), id, r.Fence); err != nil {
				return nil, err
			}
		}
		pending, encodeErr := json.Marshal(r.Pending)
		if encodeErr != nil {
			return nil, encodeErr
		}
		_, err = dbWrapper.Exec(ctx, "UPDATE source_runs SET state='cancelled',pending=?,window=NULL,producer_uuid=NULL,owner_uuid=NULL,lease_until_ms=NULL,revision=revision+1,updated_at_ms=? WHERE uuid=?", string(pending), now.UnixMilli(), id)
	}
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_run_reviews(run_uuid,revision,action,created_at_ms) VALUES(?,?,?,?)", id, revision+1, action, now.UnixMilli()); err != nil {
		return nil, err
	}
	return s.Find(ctx, id)
}
