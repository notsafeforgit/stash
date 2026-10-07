package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

type ArchiveActivityStore struct{}

func validActivityPage(page models.ArchiveActivityPage) bool {
	return page.Before >= 0 && page.Limit >= 1 && page.Limit <= 100
}

func activityTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	ret := time.UnixMilli(value.Int64).UTC()
	return &ret
}

type jobActivityRow struct {
	Sequence     int64         `db:"id"`
	UUID         string        `db:"uuid"`
	Kind         string        `db:"kind"`
	State        string        `db:"state"`
	Revision     int64         `db:"revision"`
	AttemptCount int64         `db:"fence"`
	MaxAttempts  int           `db:"max_attempts"`
	AvailableAt  int64         `db:"available_at_ms"`
	LeaseUntil   sql.NullInt64 `db:"lease_until_ms"`
	ErrorCode    string        `db:"error_code"`
	CreatedAt    int64         `db:"created_at_ms"`
	UpdatedAt    int64         `db:"updated_at_ms"`
}

func (row jobActivityRow) resolve() models.ArchiveJobActivity {
	return models.ArchiveJobActivity{Sequence: row.Sequence, UUID: row.UUID, Kind: row.Kind, State: row.State,
		Revision: row.Revision, AttemptCount: row.AttemptCount, MaxAttempts: row.MaxAttempts,
		AvailableAt: time.UnixMilli(row.AvailableAt).UTC(), LeaseUntil: activityTime(row.LeaseUntil), ErrorCode: row.ErrorCode,
		CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC()}
}

const jobActivitySelect = `SELECT id,uuid,kind,state,revision,fence,max_attempts,available_at_ms,lease_until_ms,error_code,created_at_ms,updated_at_ms FROM archive_jobs`

// Fixed SQL fragments are chosen by validated filters. Each combination can
// traverse its index in sequence order without sorting or reading worker bodies.
func jobActivityQuery(filter models.ArchiveJobActivityFilter) (string, []any, error) {
	if !validActivityPage(filter.ArchiveActivityPage) || (filter.Kind != "" && !validJobKind(filter.Kind)) || (filter.State != "" && !validJobState(filter.State)) {
		return "", nil, models.ErrArchiveActivityInvalid
	}
	query := jobActivitySelect
	var conditions []string
	var args []any
	if filter.Kind != "" {
		conditions = append(conditions, "kind=?")
		args = append(args, filter.Kind)
	}
	if filter.State != "" {
		conditions = append(conditions, "state=?")
		args = append(args, filter.State)
	}
	return activityPageQuery(query, "id", conditions, args, filter.ArchiveActivityPage)
}

func activityPageQuery(query, sequence string, conditions []string, args []any, page models.ArchiveActivityPage) (string, []any, error) {
	if page.Before != 0 {
		conditions = append(conditions, sequence+"<?")
		args = append(args, page.Before)
	}
	if len(conditions) != 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY " + sequence + " DESC LIMIT ?"
	args = append(args, page.Limit)
	return query, args, nil
}

func (s *ArchiveActivityStore) Jobs(ctx context.Context, filter models.ArchiveJobActivityFilter) ([]models.ArchiveJobActivity, error) {
	query, args, err := jobActivityQuery(filter)
	if err != nil {
		return nil, err
	}
	var rows []jobActivityRow
	if err := dbWrapper.Select(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	ret := make([]models.ArchiveJobActivity, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret, nil
}

func (s *ArchiveActivityStore) Job(ctx context.Context, id string) (*models.ArchiveJobActivity, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrArchiveActivityInvalid
	}
	var row jobActivityRow
	if err := dbWrapper.Get(ctx, &row, jobActivitySelect+" WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	ret := row.resolve()
	return &ret, nil
}

type runActivityRow struct {
	Sequence           int64         `db:"id"`
	UUID               string        `db:"uuid"`
	CollectionUUID     string        `db:"collection_uuid"`
	CollectionRevision int           `db:"collection_revision"`
	CollectionLabel    string        `db:"label"`
	TargetURL          string        `db:"target_url"`
	Operation          string        `db:"operation"`
	State              string        `db:"state"`
	Revision           int64         `db:"revision"`
	AttemptCount       int64         `db:"fence"`
	Failures           int           `db:"failures"`
	PendingWindows     int           `db:"pending_windows"`
	CompletedWindows   int           `db:"completed_windows"`
	AvailableAt        int64         `db:"available_at_ms"`
	LeaseUntil         sql.NullInt64 `db:"lease_until_ms"`
	ErrorCode          string        `db:"error_code"`
	CreatedAt          int64         `db:"created_at_ms"`
	UpdatedAt          int64         `db:"updated_at_ms"`
}

func (row runActivityRow) resolve() models.SourceRunActivity {
	return models.SourceRunActivity{Sequence: row.Sequence, UUID: row.UUID, CollectionUUID: row.CollectionUUID,
		CollectionRevision: row.CollectionRevision, CollectionLabel: row.CollectionLabel, TargetURL: row.TargetURL,
		Operation: row.Operation, State: row.State, Revision: row.Revision, AttemptCount: row.AttemptCount, Failures: row.Failures,
		PendingWindows: row.PendingWindows, CompletedWindows: row.CompletedWindows,
		AvailableAt: time.UnixMilli(row.AvailableAt).UTC(), LeaseUntil: activityTime(row.LeaseUntil), ErrorCode: row.ErrorCode,
		CreatedAt: time.UnixMilli(row.CreatedAt).UTC(), UpdatedAt: time.UnixMilli(row.UpdatedAt).UTC()}
}

// Keep the captured collection revision, even if the current collection changes
// its name, target URL or root. CROSS JOIN retains the bounded run scan first.
const runActivitySelect = `SELECT r.id,r.uuid,r.collection_uuid,r.collection_revision,c.label,c.target_url,
r.operation,r.state,r.revision,r.fence,r.failures,json_array_length(r.pending) AS pending_windows,
json_array_length(r.completed) AS completed_windows,r.available_at_ms,r.lease_until_ms,r.error_code,r.created_at_ms,r.updated_at_ms
FROM source_runs r CROSS JOIN source_collection_revisions c ON c.collection_uuid=r.collection_uuid AND c.revision=r.collection_revision`

func validActivityRunState(state string) bool {
	return state == "" || state == "queued" || state == "running" || state == "succeeded" || state == "deferred" || state == "cancelled"
}

func runActivityQuery(filter models.SourceRunActivityFilter) (string, []any, error) {
	if !validActivityPage(filter.ArchiveActivityPage) || (filter.CollectionUUID != "" && !validSourceRunUUID(filter.CollectionUUID)) || !validActivityRunState(filter.State) {
		return "", nil, models.ErrArchiveActivityInvalid
	}
	var conditions []string
	var args []any
	if filter.CollectionUUID != "" {
		conditions = append(conditions, "r.collection_uuid=?")
		args = append(args, filter.CollectionUUID)
	}
	if filter.State != "" {
		conditions = append(conditions, "r.state=?")
		args = append(args, filter.State)
	}
	return activityPageQuery(runActivitySelect, "r.id", conditions, args, filter.ArchiveActivityPage)
}

func (s *ArchiveActivityStore) Runs(ctx context.Context, filter models.SourceRunActivityFilter) ([]models.SourceRunActivity, error) {
	query, args, err := runActivityQuery(filter)
	if err != nil {
		return nil, err
	}
	var rows []runActivityRow
	if err := dbWrapper.Select(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	ret := make([]models.SourceRunActivity, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret, nil
}

func (s *ArchiveActivityStore) Run(ctx context.Context, id string) (*models.SourceRunActivity, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrArchiveActivityInvalid
	}
	var row runActivityRow
	if err := dbWrapper.Get(ctx, &row, runActivitySelect+" WHERE r.uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	ret := row.resolve()
	return &ret, nil
}

func activityAttempts(ctx context.Context, sourceRun bool, id string, page models.ArchiveActivityPage) ([]models.ArchiveActivityAttempt, error) {
	if !validSourceRunUUID(id) || !validActivityPage(page) {
		return nil, models.ErrArchiveActivityInvalid
	}
	table, key := "archive_job_attempts", "job_uuid"
	if sourceRun {
		table, key = "source_run_attempts", "run_uuid"
	}
	query, args, _ := activityPageQuery("SELECT fence,started_at_ms,ended_at_ms,outcome,error_code FROM "+table,
		"fence", []string{key + "=?"}, []any{id}, page)
	var rows []struct {
		Number    int64         `db:"fence"`
		StartedAt int64         `db:"started_at_ms"`
		EndedAt   sql.NullInt64 `db:"ended_at_ms"`
		Outcome   string        `db:"outcome"`
		ErrorCode string        `db:"error_code"`
	}
	if err := dbWrapper.Select(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	ret := make([]models.ArchiveActivityAttempt, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, models.ArchiveActivityAttempt{Number: row.Number, StartedAt: time.UnixMilli(row.StartedAt).UTC(),
			EndedAt: activityTime(row.EndedAt), Outcome: row.Outcome, ErrorCode: row.ErrorCode})
	}
	return ret, nil
}

func (s *ArchiveActivityStore) JobAttempts(ctx context.Context, id string, page models.ArchiveActivityPage) ([]models.ArchiveActivityAttempt, error) {
	return activityAttempts(ctx, false, id, page)
}

func (s *ArchiveActivityStore) RunAttempts(ctx context.Context, id string, page models.ArchiveActivityPage) ([]models.ArchiveActivityAttempt, error) {
	return activityAttempts(ctx, true, id, page)
}

func validateArchiveActivitySchema(conn *sqlx.DB) error {
	for _, name := range []string{"archive_jobs_kind_history", "archive_jobs_state_history", "source_runs_state_history", "source_runs_collection_state_history"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='index' AND name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	return nil
}
