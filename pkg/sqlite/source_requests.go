package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

type sourceRequestRow struct {
	RunUUID     string        `db:"run_uuid"`
	Fence       int64         `db:"fence"`
	RequestUUID string        `db:"request_uuid"`
	Scope       string        `db:"scope"`
	Class       string        `db:"class"`
	Operation   string        `db:"operation"`
	RequestedAt int64         `db:"requested_at_ms"`
	TouchedAt   int64         `db:"touched_at_ms"`
	GrantedAt   sql.NullInt64 `db:"granted_at_ms"`
	Reported    bool          `db:"reported"`
	Backoff     bool          `db:"backoff"`
}

func requestInterval(scope string) int64 {
	switch scope {
	case "service:twitter":
		return 20000
	case "service:reddit":
		return 5000
	default:
		return 1000
	}
}

func (s *SourceRunStore) RequestSource(ctx context.Context, lease models.SourceRunLease, input models.SourceRequestInput, now time.Time) (*models.SourceRequestPermit, error) {
	if _, err := archiveUUID(input.RequestUUID); err != nil {
		return nil, models.ErrSourceRunInvalid
	}
	switch input.Class {
	case "routine", "initial", "recovery":
	default:
		return nil, models.ErrSourceRunInvalid
	}
	switch input.Operation {
	case "listing", "profile", "media-resolution", "other":
	default:
		return nil, models.ErrSourceRunInvalid
	}
	reservation, err := s.ReserveSource(ctx, lease, input.URL, now)
	if err != nil {
		return nil, err
	}
	result := &models.SourceRequestPermit{RequestUUID: input.RequestUUID, Scope: reservation.Scope, WaitMS: 5000}
	scope, stamp := reservation.Scope, now.UnixMilli()
	if !reservation.Ready {
		return result, nil
	}
	complete := sourcePacingAtomic(ctx)
	if _, err := dbWrapper.Exec(ctx, "INSERT OR IGNORE INTO source_request_budgets(scope) VALUES(?)", scope); err != nil {
		return nil, err
	}
	var row sourceRequestRow
	err = dbWrapper.Get(ctx, &row, "SELECT * FROM source_request_permits WHERE run_uuid=?", lease.RunUUID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && row.Fence == lease.Fence && row.RequestUUID == input.RequestUUID {
		if row.Scope != scope || row.Class != input.Class || row.Operation != input.Operation || row.Reported {
			return nil, models.ErrSourceRunConflict
		}
		if row.GrantedAt.Valid {
			// A lost permit response can be retried, but stale grants must not bypass
			// the current budget after an arbitrarily long client outage.
			if stamp-row.GrantedAt.Int64 > 30000 {
				return nil, models.ErrSourceRunConflict
			}
			result.Ready, result.WaitMS = true, 0
			*complete = true
			return result, nil
		}
	} else {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_request_permits(run_uuid,fence,request_uuid,scope,class,operation,requested_at_ms,touched_at_ms)
   VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(run_uuid) DO UPDATE SET fence=excluded.fence,request_uuid=excluded.request_uuid,
   scope=excluded.scope,class=excluded.class,operation=excluded.operation,requested_at_ms=excluded.requested_at_ms,
   touched_at_ms=excluded.touched_at_ms,granted_at_ms=NULL,reported=0`, lease.RunUUID, lease.Fence, input.RequestUUID, scope, input.Class, input.Operation, stamp, stamp); err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE source_request_permits SET touched_at_ms=? WHERE run_uuid=?", stamp, lease.RunUUID); err != nil {
		return nil, err
	}
	wait, err := sourceRequestWait(ctx, scope, input.RequestUUID, now)
	if err != nil {
		return nil, err
	}
	if wait > 0 {
		result.WaitMS = wait
		*complete = true
		return result, nil
	}
	if input.Class != "routine" {
		var routine bool
		if err := dbWrapper.Get(ctx, &routine, `SELECT EXISTS(SELECT 1 FROM source_request_permits p JOIN source_runs r ON r.uuid=p.run_uuid AND r.fence=p.fence
   WHERE p.scope=? AND p.class IN ('routine', CASE WHEN ?='recovery' THEN 'initial' ELSE 'routine' END) AND p.granted_at_ms IS NULL AND p.touched_at_ms>? AND r.state='running' AND r.lease_until_ms>?)`, scope, input.Class, stamp-60000, stamp); err != nil {
			return nil, err
		}
		if routine {
			*complete = true
			return result, nil
		}
	}
	probe, err := sourceRequestGranted(ctx, scope, input.RequestUUID, now)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE source_request_permits SET granted_at_ms=?,backoff=? WHERE run_uuid=?", stamp, probe, lease.RunUUID); err != nil {
		return nil, err
	}
	hour := stamp / 3600000
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_request_metrics(hour,scope,class,operation,requests) VALUES(?,?,?,?,1)
  ON CONFLICT(hour,scope,class,operation) DO UPDATE SET requests=requests+1`, hour, scope, input.Class, input.Operation); err != nil {
		return nil, err
	}
	var maintained int64
	if err := dbWrapper.Get(ctx, &maintained, "SELECT maintained_hour FROM source_request_budgets WHERE scope=?", scope); err != nil {
		return nil, err
	}
	if maintained < hour {
		if _, err := dbWrapper.Exec(ctx, `DELETE FROM metadata_request_permits WHERE granted_at_ms<? AND NOT EXISTS(SELECT 1 FROM archive_jobs j WHERE j.uuid=job_uuid AND j.state='running')`, stamp-86400000); err != nil {
			return nil, err
		}
		for _, query := range []string{"DELETE FROM source_request_metrics WHERE hour<?", "DELETE FROM source_request_posts WHERE hour<?"} {
			if _, err := dbWrapper.Exec(ctx, query, hour-24*30); err != nil {
				return nil, err
			}
		}
		if _, err := dbWrapper.Exec(ctx, `DELETE FROM source_request_permits WHERE touched_at_ms<? AND NOT EXISTS(SELECT 1 FROM source_runs r WHERE r.uuid=run_uuid AND r.state='running')`, stamp-86400000); err != nil {
			return nil, err
		}
		if _, err := dbWrapper.Exec(ctx, "UPDATE source_request_budgets SET maintained_hour=?", hour); err != nil {
			return nil, err
		}
	}
	result.Ready, result.WaitMS = true, 0
	*complete = true
	return result, nil
}

func (s *SourceRunStore) ReportSourceRequest(ctx context.Context, lease models.SourceRunLease, input models.SourceRequestResult, now time.Time) error {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return err
	}
	if !validSourceRequestResult(input) {
		return models.ErrSourceRunInvalid
	}
	if _, err := s.CheckLease(ctx, lease, now); err != nil {
		return err
	}
	var row sourceRequestRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_request_permits WHERE run_uuid=? AND fence=? AND request_uuid=? AND granted_at_ms IS NOT NULL", lease.RunUUID, lease.Fence, input.RequestUUID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrSourceRunConflict
		}
		return err
	}
	if input.BlockReason != "" && row.Scope != "service:reddit" {
		return models.ErrSourceRunInvalid
	}
	if row.Reported {
		return nil
	}
	complete := sourcePacingAtomic(ctx)
	if _, err := dbWrapper.Exec(ctx, `UPDATE source_request_metrics SET responses=responses+1,rate_limited=rate_limited+?,errors=errors+?
  WHERE hour=? AND scope=? AND class=? AND operation=?`, input.Status == 429 || input.BlockReason != "", input.Status == 0 || input.Status >= 400 || input.BlockReason != "", row.GrantedAt.Int64/3600000, row.Scope, row.Class, row.Operation); err != nil {
		return err
	}
	backoff, err := sourceRequestFeedback(ctx, row.Scope, row.GrantedAt.Int64, row.Backoff, input, now)
	if err != nil {
		return err
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE source_request_permits SET reported=1,backoff=? WHERE run_uuid=?", backoff, lease.RunUUID); err != nil {
		return err
	}
	*complete = true
	return nil
}

func (s *SourceRunStore) RecordPostMetric(ctx context.Context, run, post string, isNew bool, now time.Time) error {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return err
	}
	if run == "" {
		return nil
	}
	var row sourceRequestRow
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM source_request_permits WHERE run_uuid=?", run)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	hour := now.UnixMilli() / 3600000
	inserted, err := dbWrapper.Exec(ctx, "INSERT OR IGNORE INTO source_request_posts(run_uuid,post_uuid,hour) VALUES(?,?,?)", run, post, hour)
	if err != nil {
		return err
	}
	n, err := inserted.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	// Posts are attributed to their root listing service, even when the last
	// HTTP request resolved media on an external host.
	scope, err := sourcePacingScope(ctx, run, false)
	if err != nil {
		return err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_request_metrics(hour,scope,class,operation,new_posts,repeated_posts) VALUES(?,?,?,'posts',?,?)
  ON CONFLICT(hour,scope,class,operation) DO UPDATE SET new_posts=new_posts+excluded.new_posts,repeated_posts=repeated_posts+excluded.repeated_posts`, hour, scope, row.Class, isNew, !isNew)
	return err
}

func (s *SourceRunStore) RequestMetrics(ctx context.Context, now time.Time, offset int) (*models.SourceRequestSummary, error) {
	if offset < 0 || offset > 1000000 {
		return nil, models.ErrSourceRunInvalid
	}
	result := &models.SourceRequestSummary{Since: now.Truncate(time.Hour).Add(-24 * time.Hour), Metrics: []models.SourceRequestMetric{}, Accounts: []models.SourceCheckHealth{}}
	err := dbWrapper.Select(ctx, &result.Metrics, `SELECT scope,class,operation,sum(requests) requests,sum(responses) responses,sum(rate_limited) rate_limited,sum(errors) errors,sum(new_posts) new_posts,sum(repeated_posts) repeated_posts
 FROM source_request_metrics WHERE hour>=? GROUP BY scope,class,operation ORDER BY scope,class,operation`, result.Since.UnixMilli()/3600000)
	if err != nil {
		return nil, err
	}
	const base = ` FROM source_collections c JOIN source_collection_revisions d ON d.collection_uuid=c.uuid AND d.revision=c.revision
 LEFT JOIN source_check_health h ON h.collection_uuid=c.uuid WHERE d.state='active' AND d.kind='account'`
	if err := dbWrapper.Get(ctx, &result.Total, "SELECT count(*)"+base); err != nil {
		return nil, err
	}
	err = dbWrapper.Select(ctx, &result.Accounts, `SELECT c.uuid collection_uuid,d.label label,d.target_url url,coalesce(h.last_success_ms,0) last_success_ms,
 CASE WHEN h.last_success_ms IS NULL THEN 0 ELSE max(0,(?-h.last_success_ms)/1000-86400) END overdue_seconds`+base+` ORDER BY coalesce(h.last_success_ms,0),c.uuid LIMIT 50 OFFSET ?`, now.UnixMilli(), offset)
	return result, err
}
