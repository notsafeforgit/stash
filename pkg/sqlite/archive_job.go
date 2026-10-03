package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	hexencoding "encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type ArchiveJobStore struct{}

type archiveJobRow struct {
	Sequence    int64          `db:"id"`
	UUID        string         `db:"uuid"`
	Kind        string         `db:"kind"`
	WorkKey     string         `db:"work_key"`
	ResourceKey string         `db:"resource_key"`
	Arguments   string         `db:"arguments"`
	State       string         `db:"state"`
	Revision    int64          `db:"revision"`
	Priority    int            `db:"priority"`
	Fence       int64          `db:"fence"`
	MaxAttempts int            `db:"max_attempts"`
	AvailableAt int64          `db:"available_at_ms"`
	Owner       sql.NullString `db:"owner_uuid"`
	LeaseUntil  sql.NullInt64  `db:"lease_until_ms"`
	Progress    string         `db:"progress"`
	Result      string         `db:"result"`
	ErrorCode   string         `db:"error_code"`
	CreatedAt   int64          `db:"created_at_ms"`
	UpdatedAt   int64          `db:"updated_at_ms"`
}

func (r archiveJobRow) resolve() *models.ArchiveJob {
	j := &models.ArchiveJob{Sequence: r.Sequence, UUID: r.UUID, Kind: r.Kind, WorkKey: r.WorkKey, ResourceKey: r.ResourceKey,
		Arguments: json.RawMessage(r.Arguments), State: r.State, Revision: r.Revision, Priority: r.Priority, Fence: r.Fence,
		MaxAttempts: r.MaxAttempts, AvailableAt: time.UnixMilli(r.AvailableAt).UTC(), OwnerUUID: r.Owner.String,
		Progress: json.RawMessage(r.Progress), Result: json.RawMessage(r.Result), ErrorCode: r.ErrorCode,
		CreatedAt: time.UnixMilli(r.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(r.UpdatedAt).UTC()}
	if r.LeaseUntil.Valid {
		value := time.UnixMilli(r.LeaseUntil.Int64).UTC()
		j.LeaseUntil = &value
	}
	return j
}

func managedArchiveJobWrite(ctx context.Context) error {
	if _, err := getTx(ctx); err != nil {
		return err
	}
	if writable, _ := ctx.Value(writableKey).(bool); !writable || !txn.HasHooks(ctx) {
		return errors.New("archive jobs require a managed write transaction")
	}
	return nil
}

func validJobTime(value time.Time) bool { return value.UnixMilli() > 0 && value.UTC().Year() <= 9999 }
func validJobKind(kind string) bool {
	return kind == models.ArchiveJobVerifyMedia || kind == models.ArchiveJobBackfillAlbum || kind == models.ArchiveJobTranslateText || kind == models.ArchiveJobEnrichPost
}
func validJobState(state string) bool {
	return state == "queued" || state == "running" || state == "succeeded" || state == "failed" || state == "cancelled"
}
func jobJSON(raw json.RawMessage, limit int) (json.RawMessage, error) {
	tree, err := archive.DecodeJSONObject(raw, limit)
	if err != nil {
		return nil, errors.New("invalid archive job JSON object")
	}
	return archive.EncodeSourceJSON(tree)
}
func jobErrorCode(value string) bool {
	return len(value) <= 128 && strings.IndexFunc(value, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '.' && r != '-'
	}) == -1
}

func prepareJobSubmission(input models.ArchiveJobSubmission) (models.ArchiveJobSubmission, string, error) {
	id, err := archiveUUID(input.RequestUUID)
	if err != nil || id != input.RequestUUID || !validJobKind(input.Kind) || !archive.ValidSHA256(input.WorkKey) || !archive.ValidSHA256(input.ResourceKey) ||
		input.Priority < 0 || input.Priority > 100 || input.MaxAttempts < 1 || input.MaxAttempts > 100 || (!input.AvailableAt.IsZero() && !validJobTime(input.AvailableAt)) {
		return input, "", errors.New("invalid archive job submission")
	}
	input.Arguments, err = jobJSON(input.Arguments, 262144)
	if err != nil {
		return input, "", err
	}
	if !input.AvailableAt.IsZero() {
		input.AvailableAt = input.AvailableAt.UTC().Truncate(time.Millisecond)
	}
	body, err := json.Marshal(input)
	if err != nil {
		return input, "", err
	}
	digest := sha256.Sum256(body)
	return input, hexencoding.EncodeToString(digest[:]), nil
}

func (s *ArchiveJobStore) Find(ctx context.Context, id string) (*models.ArchiveJob, error) {
	id, err := archiveUUID(id)
	if err != nil {
		return nil, err
	}
	return findArchiveJob(ctx, "SELECT * FROM archive_jobs WHERE uuid=?", id)
}
func (s *ArchiveJobStore) FindSubmission(ctx context.Context, id string) (*models.ArchiveJob, error) {
	id, err := archiveUUID(id)
	if err != nil {
		return nil, err
	}
	return findArchiveJob(ctx, `SELECT j.* FROM archive_job_submissions s JOIN archive_jobs j ON j.uuid=s.job_uuid WHERE s.request_uuid=?`, id)
}
func findArchiveJob(ctx context.Context, query string, args ...interface{}) (*models.ArchiveJob, error) {
	var row archiveJobRow
	if err := dbWrapper.Get(ctx, &row, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func (s *ArchiveJobStore) Submit(ctx context.Context, input models.ArchiveJobSubmission, now time.Time, maxActive int) (*models.ArchiveJob, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) || maxActive < 1 || maxActive > 100000 {
		return nil, errors.New("invalid archive job queue limits")
	}
	input, digest, err := prepareJobSubmission(input)
	if err != nil {
		return nil, err
	}
	if input.Kind == models.ArchiveJobTranslateText {
		translationJobSubmissionGuard(ctx, input.RequestUUID)
	}
	if input.Kind == models.ArchiveJobEnrichPost {
		enrichmentJobSubmissionGuard(ctx, input.RequestUUID)
	}
	var previous struct {
		Digest string `db:"digest"`
		Job    string `db:"job_uuid"`
	}
	err = dbWrapper.Get(ctx, &previous, "SELECT digest,job_uuid FROM archive_job_submissions WHERE request_uuid=?", input.RequestUUID)
	if err == nil {
		if previous.Digest != digest {
			return nil, models.ErrArchiveJobConflict
		}
		return s.Find(ctx, previous.Job)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	job, err := findArchiveJob(ctx, "SELECT * FROM archive_jobs WHERE kind=? AND work_key=? AND state IN ('queued','running')", input.Kind, input.WorkKey)
	if err != nil {
		return nil, err
	}
	available := input.AvailableAt.UnixMilli()
	if input.AvailableAt.IsZero() {
		available = now.UnixMilli()
	}
	if job != nil {
		if job.ResourceKey != input.ResourceKey || string(job.Arguments) != string(input.Arguments) || job.MaxAttempts != input.MaxAttempts {
			return nil, models.ErrArchiveJobConflict
		}
		// Repeated submissions may promote pending work, but must not bypass a
		// worker's retry delay or a source's deferral once work has been attempted.
		if job.Fence > 0 {
			available = job.AvailableAt.UnixMilli()
		}
		if job.State == "queued" && (input.Priority > job.Priority || available < job.AvailableAt.UnixMilli()) {
			_, err = dbWrapper.Exec(ctx, `UPDATE archive_jobs SET priority=max(priority,?),available_at_ms=min(available_at_ms,?),revision=revision+1,updated_at_ms=? WHERE uuid=?`, input.Priority, available, now.UnixMilli(), job.UUID)
			if err != nil {
				return nil, err
			}
		}
	} else {
		var active int
		if input.Kind == models.ArchiveJobEnrichPost {
			if err := dbWrapper.Get(ctx, &active, "SELECT count(*) FROM (SELECT 1 FROM archive_jobs WHERE kind='post.enrich' AND state IN ('queued','running') LIMIT ?)", archive.MaxEnrichmentJobs); err != nil {
				return nil, err
			}
			if active >= archive.MaxEnrichmentJobs {
				return nil, models.ErrArchiveJobCapacity
			}
		}
		if err := dbWrapper.Get(ctx, &active, "SELECT count(*) FROM (SELECT 1 FROM archive_jobs WHERE state IN ('queued','running') LIMIT ?)", maxActive); err != nil {
			return nil, err
		}
		if active >= maxActive {
			return nil, models.ErrArchiveJobCapacity
		}
		job = &models.ArchiveJob{UUID: uuid.NewString()}
		_, err = dbWrapper.Exec(ctx, `INSERT INTO archive_jobs(uuid,kind,work_key,resource_key,arguments,priority,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?)`, job.UUID, input.Kind, input.WorkKey, input.ResourceKey, string(input.Arguments), input.Priority, input.MaxAttempts, available, now.UnixMilli(), now.UnixMilli())
		if err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO archive_job_submissions(request_uuid,digest,job_uuid,created_at_ms) VALUES(?,?,?,?)", input.RequestUUID, digest, job.UUID, now.UnixMilli()); err != nil {
		return nil, err
	}
	return s.Find(ctx, job.UUID)
}

func (s *ArchiveJobStore) List(ctx context.Context, kind, state string, after int64, limit int) ([]models.ArchiveJob, error) {
	if !validJobKind(kind) || !validJobState(state) || after < 0 {
		return nil, errors.New("invalid archive job listing")
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	var rows []archiveJobRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM archive_jobs WHERE kind=? AND state=? AND id>? ORDER BY id LIMIT ?", kind, state, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.ArchiveJob, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, *row.resolve())
	}
	return ret, nil
}

func (s *ArchiveJobStore) ResourceHistory(ctx context.Context, kind, resource string, after int64, limit int) ([]models.ArchiveJob, error) {
	if !validJobKind(kind) || !archive.ValidSHA256(resource) || after < 0 {
		return nil, errors.New("invalid archive job resource history")
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	var rows []archiveJobRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM archive_jobs WHERE kind=? AND resource_key=? AND id>? ORDER BY id LIMIT ?", kind, resource, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.ArchiveJob, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, *row.resolve())
	}
	return ret, nil
}

func (s *ArchiveJobStore) Attempts(ctx context.Context, id string, after int64, limit int) ([]models.ArchiveJobAttempt, error) {
	id, err := archiveUUID(id)
	if err != nil || after < 0 {
		return nil, errors.New("invalid archive job attempt cursor")
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		JobUUID string        `db:"job_uuid"`
		Fence   int64         `db:"fence"`
		Owner   string        `db:"owner_uuid"`
		Started int64         `db:"started_at_ms"`
		Ended   sql.NullInt64 `db:"ended_at_ms"`
		Outcome string        `db:"outcome"`
		Result  string        `db:"result"`
		Error   string        `db:"error_code"`
	}
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM archive_job_attempts WHERE job_uuid=? AND fence>? ORDER BY fence LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.ArchiveJobAttempt, 0, len(rows))
	for _, row := range rows {
		attempt := models.ArchiveJobAttempt{JobUUID: row.JobUUID, Fence: row.Fence, OwnerUUID: row.Owner, StartedAt: time.UnixMilli(row.Started).UTC(), Outcome: row.Outcome, Result: json.RawMessage(row.Result), ErrorCode: row.Error}
		if row.Ended.Valid {
			ended := time.UnixMilli(row.Ended.Int64).UTC()
			attempt.EndedAt = &ended
		}
		ret = append(ret, attempt)
	}
	return ret, nil
}

func validJobLease(now time.Time, duration time.Duration) bool {
	return validJobTime(now) && duration >= 5*time.Second && duration <= 15*time.Minute && validJobTime(now.Add(duration))
}

func (s *ArchiveJobStore) Claim(ctx context.Context, kind, owner string, now time.Time, duration time.Duration) (*models.ArchiveJob, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	id, err := archiveUUID(owner)
	if err != nil || id != owner || !validJobKind(kind) || !validJobLease(now, duration) {
		return nil, errors.New("invalid archive job claim")
	}
	if _, err := s.Recover(ctx, now, 100); err != nil {
		return nil, err
	}
	job, err := findArchiveJob(ctx, `SELECT j.* FROM archive_jobs j INDEXED BY archive_jobs_ready
WHERE j.kind=? AND j.state='queued' AND j.available_at_ms<=? AND j.fence<j.max_attempts
 AND NOT EXISTS(SELECT 1 FROM archive_jobs r WHERE r.state='running' AND r.resource_key=j.resource_key)
ORDER BY j.priority DESC,j.available_at_ms,j.id LIMIT 1`, kind, now.UnixMilli())
	if err != nil || job == nil {
		return job, err
	}
	return s.claim(ctx, job, owner, now, duration)
}

func (s *ArchiveJobStore) ClaimByID(ctx context.Context, id string, revision int64, owner string, now time.Time, duration time.Duration) (*models.ArchiveJob, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	jobID, err := archiveUUID(id)
	if err != nil || jobID != id || revision < 1 || !validJobLease(now, duration) {
		return nil, errors.New("invalid selected archive job claim")
	}
	ownerID, err := archiveUUID(owner)
	if err != nil || ownerID != owner {
		return nil, errors.New("invalid selected archive job owner")
	}
	current, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Revision != revision {
		return nil, models.ErrArchiveJobConflict
	}
	job, err := findArchiveJob(ctx, `SELECT j.* FROM archive_jobs j
WHERE j.uuid=? AND j.state='queued' AND j.available_at_ms<=? AND j.fence<j.max_attempts
 AND NOT EXISTS(SELECT 1 FROM archive_jobs r WHERE r.state='running' AND r.resource_key=j.resource_key)`, id, now.UnixMilli())
	if err != nil || job == nil {
		return job, err
	}
	return s.claim(ctx, job, owner, now, duration)
}

func (s *ArchiveJobStore) claim(ctx context.Context, job *models.ArchiveJob, owner string, now time.Time, duration time.Duration) (*models.ArchiveJob, error) {
	if job.Kind == models.ArchiveJobEnrichPost {
		enrichmentJobAttemptGuard(ctx, job.UUID, job.Fence+1)
	}
	// Do not commit the running head without its attempt, even if a caller
	// catches a later error and tries to commit unrelated domain writes.
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrArchiveJobConflict
		}
		return nil
	})
	_, err := dbWrapper.Exec(ctx, `UPDATE archive_jobs SET state='running',fence=fence+1,owner_uuid=?,lease_until_ms=?,result='{}',error_code='',revision=revision+1,updated_at_ms=? WHERE uuid=?`, owner, now.Add(duration).UnixMilli(), now.UnixMilli(), job.UUID)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO archive_job_attempts(job_uuid,fence,owner_uuid,started_at_ms) VALUES(?,?,?,?)", job.UUID, job.Fence+1, owner, now.UnixMilli()); err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, job.UUID)
	complete = err == nil && ret != nil
	return ret, err
}

func (s *ArchiveJobStore) CheckLease(ctx context.Context, lease models.ArchiveJobLease, now time.Time) (*models.ArchiveJob, error) {
	if !validJobTime(now) || lease.Fence < 1 {
		return nil, models.ErrArchiveJobLease
	}
	owner, err := archiveUUID(lease.OwnerUUID)
	if err != nil || owner != lease.OwnerUUID {
		return nil, models.ErrArchiveJobLease
	}
	job, err := s.Find(ctx, lease.JobUUID)
	if err != nil {
		return nil, err
	}
	if job == nil || job.State != "running" || job.OwnerUUID != owner || job.Fence != lease.Fence || job.LeaseUntil == nil || !now.Before(*job.LeaseUntil) {
		return nil, models.ErrArchiveJobLease
	}
	return job, nil
}

func (s *ArchiveJobStore) Renew(ctx context.Context, lease models.ArchiveJobLease, now time.Time, duration time.Duration) (*models.ArchiveJob, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobLease(now, duration) {
		return nil, errors.New("invalid archive job lease duration")
	}
	job, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `UPDATE archive_jobs SET lease_until_ms=max(lease_until_ms,?),revision=revision+1,updated_at_ms=? WHERE uuid=?`, now.Add(duration).UnixMilli(), now.UnixMilli(), job.UUID); err != nil {
		return nil, err
	}
	return s.Find(ctx, job.UUID)
}

func (s *ArchiveJobStore) Progress(ctx context.Context, lease models.ArchiveJobLease, now time.Time, raw json.RawMessage) (*models.ArchiveJob, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	progress, err := jobJSON(raw, 16384)
	if err != nil {
		return nil, err
	}
	job, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE archive_jobs SET progress=?,revision=revision+1,updated_at_ms=? WHERE uuid=?", string(progress), now.UnixMilli(), job.UUID); err != nil {
		return nil, err
	}
	return s.Find(ctx, job.UUID)
}

func finishJobAttempt(ctx context.Context, job *models.ArchiveJob, now time.Time, outcome, result, code string) error {
	ret, err := dbWrapper.Exec(ctx, `UPDATE archive_job_attempts SET outcome=?,ended_at_ms=?,result=?,error_code=? WHERE job_uuid=? AND fence=? AND outcome='running'`, outcome, now.UnixMilli(), result, code, job.UUID, job.Fence)
	if err != nil {
		return err
	}
	n, err := ret.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return models.ErrArchiveJobConflict
	}
	return nil
}

func (s *ArchiveJobStore) Finish(ctx context.Context, lease models.ArchiveJobLease, now time.Time, outcome models.ArchiveJobOutcome) (*models.ArchiveJob, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if (outcome.State != "succeeded" && outcome.State != "failed" && outcome.State != "retry") || !jobErrorCode(outcome.ErrorCode) ||
		(outcome.State != "succeeded" && outcome.ErrorCode == "") || (outcome.State == "succeeded" && outcome.ErrorCode != "") ||
		(outcome.State == "retry" && (!validJobTime(outcome.RetryAt) || outcome.RetryAt.Before(now))) || (outcome.State != "retry" && !outcome.RetryAt.IsZero()) {
		return nil, errors.New("invalid archive job outcome")
	}
	result, err := jobJSON(outcome.Result, 16384)
	if err != nil {
		return nil, err
	}
	job, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	state, attempt := outcome.State, outcome.State
	available := job.AvailableAt.UnixMilli()
	if state == "retry" {
		state, available = "queued", outcome.RetryAt.UnixMilli()
		if job.Kind == models.ArchiveJobEnrichPost {
			available = max(available, enrichmentRetryAt(job, now).UnixMilli())
		}
		if job.Fence >= int64(job.MaxAttempts) {
			state, attempt = "failed", "failed"
		}
	}
	if err := finishJobAttempt(ctx, job, now, attempt, string(result), outcome.ErrorCode); err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `UPDATE archive_jobs SET state=?,available_at_ms=?,result=?,error_code=?,owner_uuid=NULL,lease_until_ms=NULL,revision=revision+1,updated_at_ms=? WHERE uuid=?`, state, available, string(result), outcome.ErrorCode, now.UnixMilli(), job.UUID)
	if err != nil {
		return nil, err
	}
	return s.Find(ctx, job.UUID)
}

func (s *ArchiveJobStore) Cancel(ctx context.Context, id string, revision int64, now time.Time) (*models.ArchiveJob, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) || revision < 1 {
		return nil, models.ErrArchiveJobConflict
	}
	job, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if job == nil || job.Revision != revision || (job.State != "queued" && job.State != "running" && job.State != "cancelled") {
		return nil, models.ErrArchiveJobConflict
	}
	if job.State == "cancelled" {
		return job, nil
	}
	if job.State == "running" {
		if err := finishJobAttempt(ctx, job, now, "cancelled", "{}", "cancelled"); err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, `UPDATE archive_jobs SET state='cancelled',owner_uuid=NULL,lease_until_ms=NULL,error_code='cancelled',revision=revision+1,updated_at_ms=? WHERE uuid=?`, now.UnixMilli(), job.UUID); err != nil {
		return nil, err
	}
	return s.Find(ctx, job.UUID)
}

func (s *ArchiveJobStore) Recover(ctx context.Context, now time.Time, limit int) (int, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return 0, err
	}
	if !validJobTime(now) {
		return 0, errors.New("invalid archive job recovery time")
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return 0, err
	}
	var rows []archiveJobRow
	if err := dbWrapper.Select(ctx, &rows, "SELECT * FROM archive_jobs WHERE state='running' AND lease_until_ms<=? ORDER BY lease_until_ms,id LIMIT ?", now.UnixMilli(), limit); err != nil {
		return 0, err
	}
	for _, row := range rows {
		job := row.resolve()
		if err := finishJobAttempt(ctx, job, now, "expired", "{}", "lease_expired"); err != nil {
			return 0, err
		}
		state := "queued"
		if job.Fence >= int64(job.MaxAttempts) {
			state = "failed"
		}
		available := now.UnixMilli()
		if job.Kind == models.ArchiveJobEnrichPost {
			available = max(job.AvailableAt.UnixMilli(), enrichmentRetryAt(job, now).UnixMilli())
		}
		if _, err := dbWrapper.Exec(ctx, `UPDATE archive_jobs SET state=?,available_at_ms=?,owner_uuid=NULL,lease_until_ms=NULL,error_code='lease_expired',revision=revision+1,updated_at_ms=? WHERE uuid=?`, state, available, now.UnixMilli(), job.UUID); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}
