package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type LibraryBulkJobStore struct{}

func managedBulkWrite(ctx context.Context) error {
	if writable, _ := ctx.Value(writableKey).(bool); !writable || !txn.HasHooks(ctx) {
		return errors.New("bulk jobs require a managed write transaction")
	}
	return nil
}

func (*LibraryBulkJobStore) Admit(ctx context.Context, input models.LibraryBulkAdmission) error {
	if err := managedBulkWrite(ctx); err != nil {
		return err
	}
	if _, err := archiveUUID(input.ID); err != nil {
		return err
	}
	if input.Generation != 1 || len(input.Targets) > 100000 || input.CommandVersion < 1 ||
		len(input.CommandJSON) > models.MaxLibraryBulkBytes || len(input.NoticeJSON) > models.MaxLibraryBulkBytes ||
		len(input.ReferencesJSON) > models.MaxLibraryBulkBytes {
		return errors.New("invalid or oversized bulk admission")
	}
	var jobs, targets int
	if err := dbWrapper.Get(ctx, &jobs, "SELECT count(*) FROM library_bulk_jobs"); err != nil {
		return err
	}
	if err := dbWrapper.Get(ctx, &targets, "SELECT count(*) FROM library_bulk_targets"); err != nil {
		return err
	}
	if jobs >= models.MaxLibraryBulkJobs || targets+len(input.Targets) > models.MaxLibraryBulkTargets {
		return errors.New("bulk work limit reached; complete or cancel existing jobs before adding more")
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO library_jobs(id,status,description,progress,added_at,generation) VALUES(?,'READY',?,-1,?,?)`,
		input.ID, boundedJobText(input.Description, 1024), input.AddedAt.UnixNano(), input.Generation); err != nil {
		return err
	}
	var notice any
	if input.NoticeJSON != nil {
		notice = string(input.NoticeJSON)
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO library_bulk_jobs(job_id,command_version,command_kind,command_json,notice_json,references_json,total)
VALUES(?,?,?,?,?,?,?)`, input.ID, input.CommandVersion, input.CommandKind, string(input.CommandJSON), notice, string(input.ReferencesJSON), len(input.Targets)); err != nil {
		return err
	}
	stmt, err := dbWrapper.Prepare(ctx, "INSERT INTO library_bulk_targets(job_id,ordinal,entity_id,entity_uuid) VALUES(?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, target := range input.Targets {
		if _, err := archiveUUID(target.EntityUUID); err != nil {
			return err
		}
		if target.EntityID <= 0 {
			return errors.New("invalid bulk target")
		}
		if _, err := dbWrapper.ExecStmt(ctx, stmt, input.ID, i, target.EntityID, target.EntityUUID); err != nil {
			return err
		}
	}
	return nil
}

func (*LibraryBulkJobStore) Pending(ctx context.Context) ([]models.LibraryBulkJob, error) {
	var rows []models.LibraryBulkJob
	err := dbWrapper.Select(ctx, &rows, `SELECT bulk.*,jobs.generation FROM library_bulk_jobs AS bulk
JOIN library_jobs AS jobs ON jobs.id=bulk.job_id WHERE jobs.ended_at IS NULL ORDER BY jobs.added_at,jobs.id LIMIT 33`)
	if len(rows) > models.MaxLibraryBulkJobs {
		return nil, errors.New("saved bulk work exceeds its admission limit")
	}
	return rows, err
}

func (*LibraryBulkJobStore) Find(ctx context.Context, id string) (*models.LibraryBulkJob, error) {
	var row models.LibraryBulkJob
	err := dbWrapper.Get(ctx, &row, `SELECT bulk.*,jobs.generation FROM library_bulk_jobs AS bulk
JOIN library_jobs AS jobs ON jobs.id=bulk.job_id WHERE bulk.job_id=?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &row, err
}

func liveBulkGeneration(ctx context.Context, id string, generation int) error {
	if err := managedBulkWrite(ctx); err != nil {
		return err
	}
	var active bool
	err := dbWrapper.Get(ctx, &active, `SELECT EXISTS(SELECT 1 FROM library_jobs
WHERE id=? AND generation=? AND status='RUNNING' AND ended_at IS NULL)`, id, generation)
	if err != nil {
		return err
	}
	if !active {
		return models.ErrLibraryBulkInterrupted
	}
	return nil
}

func (*LibraryBulkJobStore) Next(ctx context.Context, id string, generation int) (*models.LibraryBulkTarget, error) {
	if err := liveBulkGeneration(ctx, id, generation); err != nil {
		return nil, err
	}
	var row models.LibraryBulkTarget
	err := dbWrapper.Get(ctx, &row, `SELECT ordinal,entity_id,entity_uuid,state,hook_attempts FROM library_bulk_targets
WHERE job_id=? AND state IN ('PENDING','NOTIFY') ORDER BY ordinal LIMIT 1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &row, err
}

func bulkChanged(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("bulk target checkpoint changed")
	}
	return nil
}

func (*LibraryBulkJobStore) Applied(ctx context.Context, id string, generation int, target models.LibraryBulkTarget) (int, error) {
	if err := liveBulkGeneration(ctx, id, generation); err != nil {
		return 0, err
	}
	if err := bulkChanged(dbWrapper.Exec(ctx, `UPDATE library_bulk_targets SET state=CASE
WHEN (SELECT notice_json FROM library_bulk_jobs WHERE job_id=?) IS NULL THEN 'DONE' ELSE 'NOTIFY' END
WHERE job_id=? AND ordinal=? AND entity_uuid=? AND state='PENDING'`, id, id, target.Ordinal, target.EntityUUID)); err != nil {
		return 0, err
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE library_bulk_jobs SET processed=processed+1,updated=updated+1 WHERE job_id=?", id); err != nil {
		return 0, err
	}
	return saveBulkProgress(ctx, id)
}

func (*LibraryBulkJobStore) Failed(ctx context.Context, id string, generation int, target models.LibraryBulkTarget, message string) (int, error) {
	if err := liveBulkGeneration(ctx, id, generation); err != nil {
		return 0, err
	}
	if err := bulkChanged(dbWrapper.Exec(ctx, `UPDATE library_bulk_targets SET state='FAILED'
WHERE job_id=? AND ordinal=? AND entity_uuid=? AND state='PENDING'`, id, target.Ordinal, target.EntityUUID)); err != nil {
		return 0, err
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE library_bulk_jobs SET processed=processed+1,failed=failed+1 WHERE job_id=?", id); err != nil {
		return 0, err
	}
	if err := appendBulkError(ctx, id, models.LibraryBulkError{EntityUUID: target.EntityUUID, Stage: "update", Message: message}); err != nil {
		return 0, err
	}
	return saveBulkProgress(ctx, id)
}

func saveBulkProgress(ctx context.Context, id string) (int, error) {
	var processed int
	if err := dbWrapper.Get(ctx, &processed, "SELECT processed FROM library_bulk_jobs WHERE job_id=?", id); err != nil {
		return 0, err
	}
	_, err := dbWrapper.Exec(ctx, `UPDATE library_jobs SET progress=(SELECT CASE WHEN total=0 THEN 1.0 ELSE 1.0*processed/total END
FROM library_bulk_jobs WHERE job_id=?) WHERE id=?`, id, id)
	return processed, err
}

func (*LibraryBulkJobStore) StartNotification(ctx context.Context, id string, generation int, target models.LibraryBulkTarget) error {
	if err := liveBulkGeneration(ctx, id, generation); err != nil {
		return err
	}
	return bulkChanged(dbWrapper.Exec(ctx, `UPDATE library_bulk_targets SET hook_attempts=hook_attempts+1
WHERE job_id=? AND ordinal=? AND entity_uuid=? AND state='NOTIFY' AND hook_attempts=? AND hook_attempts<3`,
		id, target.Ordinal, target.EntityUUID, target.HookAttempts))
}

func (*LibraryBulkJobStore) FinishNotification(ctx context.Context, id string, generation int, target models.LibraryBulkTarget, failure *string) error {
	if err := liveBulkGeneration(ctx, id, generation); err != nil {
		return err
	}
	if failure != nil && target.HookAttempts < models.MaxLibraryHookTries {
		return nil // Retry only notification delivery, never the committed edit.
	}
	state := "DONE"
	if failure != nil {
		state = "NOTIFY_FAILED"
	}
	if err := bulkChanged(dbWrapper.Exec(ctx, `UPDATE library_bulk_targets SET state=?
WHERE job_id=? AND ordinal=? AND entity_uuid=? AND state='NOTIFY' AND hook_attempts=? AND hook_attempts>0`,
		state, id, target.Ordinal, target.EntityUUID, target.HookAttempts)); err != nil {
		return err
	}
	if failure != nil {
		if _, err := dbWrapper.Exec(ctx, "UPDATE library_bulk_jobs SET notification_failed=notification_failed+1 WHERE job_id=?", id); err != nil {
			return err
		}
		return appendBulkError(ctx, id, models.LibraryBulkError{EntityUUID: target.EntityUUID, Stage: "notification", Message: *failure})
	}
	return nil
}

func appendBulkError(ctx context.Context, id string, sample models.LibraryBulkError) error {
	var data []byte
	if err := dbWrapper.Get(ctx, &data, "SELECT errors_json FROM library_bulk_jobs WHERE job_id=?", id); err != nil {
		return err
	}
	var samples []models.LibraryBulkError
	if err := json.Unmarshal(data, &samples); err != nil {
		return err
	}
	if len(samples) >= models.MaxLibraryBulkErrors {
		return nil
	}
	sample.Message = boundedJobText(sample.Message, 1024)
	data, err := json.Marshal(append(samples, sample))
	if err != nil {
		return err
	}
	if len(data) > 32768 {
		return nil // Escaped/control characters must also fit the byte budget.
	}
	_, err = dbWrapper.Exec(ctx, "UPDATE library_bulk_jobs SET errors_json=? WHERE job_id=?", string(data), id)
	return err
}

func boundedJobText(value string, limit int) string {
	value = strings.ToValidUTF8(value, "?")
	if len(value) <= limit {
		return value
	}
	end := limit - 3
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return fmt.Sprintf("%s...", value[:end])
}
