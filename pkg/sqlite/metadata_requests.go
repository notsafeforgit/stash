package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type metadataRequestRow struct {
	JobUUID     string `db:"job_uuid"`
	Fence       int64  `db:"fence"`
	RequestUUID string `db:"request_uuid"`
	Scope       string `db:"scope"`
	Operation   string `db:"operation"`
	GrantedAt   int64  `db:"granted_at_ms"`
	Reported    bool   `db:"reported"`
}

// Metadata gets idle capacity only. Recheck downloads on every HTTP attempt,
// including child resolution, so a previously claimed job cannot reserve a site.
func (s *EnrichmentJobStore) RequestSource(ctx context.Context, lease models.EnrichmentJobLease, input models.SourceRequestInput, now time.Time) (*models.SourceRequestPermit, error) {
	if _, err := archiveUUID(input.RequestUUID); err != nil || input.Class != "recovery" {
		return nil, models.ErrEnrichmentInvalid
	}
	switch input.Operation {
	case "listing", "profile", "media-resolution", "other":
	default:
		return nil, models.ErrEnrichmentInvalid
	}
	scope, err := scrape.SourceScopeV1(input.URL)
	if err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	job, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	work, err := archive.DecodeEnrichmentJob(job)
	if err != nil {
		return nil, err
	}
	root, err := sourcePacingScope(ctx, job.UUID, true)
	if err != nil {
		return nil, err
	}
	result := &models.SourceRequestPermit{RequestUUID: input.RequestUUID, Scope: scope}
	ready, err := sourcePacingReadyFor(ctx, root, work.CollectionUUID, true, job.UUID, "", now)
	if err != nil {
		return nil, err
	}
	if !ready {
		return result, nil
	}
	ready, err = s.ReserveSource(ctx, lease, input.URL, now)
	if err != nil {
		return nil, err
	}
	if !ready {
		return result, nil
	} // zero wait means yield to monitoring
	var old metadataRequestRow
	err = dbWrapper.Get(ctx, &old, "SELECT * FROM metadata_request_permits WHERE job_uuid=?", lease.JobUUID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && old.Fence == lease.Fence && old.RequestUUID == input.RequestUUID {
		if old.Scope != scope || old.Operation != input.Operation || old.Reported || now.UnixMilli()-old.GrantedAt > 30000 {
			return nil, models.ErrEnrichmentConflict
		}
		result.Ready = true
		return result, nil
	}
	complete := sourcePacingAtomic(ctx)
	if _, err := dbWrapper.Exec(ctx, "INSERT OR IGNORE INTO source_request_budgets(scope) VALUES(?)", scope); err != nil {
		return nil, err
	}
	var next int64
	if err := dbWrapper.Get(ctx, &next, "SELECT next_at_ms FROM source_request_budgets WHERE scope=?", scope); err != nil {
		return nil, err
	}
	if next > now.UnixMilli() {
		result.WaitMS = min(30000, next-now.UnixMilli())
		*complete = true
		return result, nil
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO metadata_request_permits(job_uuid,fence,request_uuid,scope,operation,granted_at_ms)
 VALUES(?,?,?,?,?,?) ON CONFLICT(job_uuid) DO UPDATE SET fence=excluded.fence,request_uuid=excluded.request_uuid,
 scope=excluded.scope,operation=excluded.operation,granted_at_ms=excluded.granted_at_ms,reported=0`, lease.JobUUID, lease.Fence, input.RequestUUID, scope, input.Operation, now.UnixMilli()); err != nil {
		return nil, err
	}
	// The same spacing and Retry-After budget governs both producer types.
	if _, err := dbWrapper.Exec(ctx, "UPDATE source_request_budgets SET next_at_ms=? WHERE scope=?", now.UnixMilli()+requestInterval(scope), scope); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_request_metrics(hour,scope,class,operation,requests) VALUES(?,?,'recovery',?,1)
 ON CONFLICT(hour,scope,class,operation) DO UPDATE SET requests=requests+1`, now.UnixMilli()/3600000, scope, input.Operation); err != nil {
		return nil, err
	}
	result.Ready = true
	*complete = true
	return result, nil
}

func (s *EnrichmentJobStore) ReportSourceRequest(ctx context.Context, lease models.EnrichmentJobLease, input models.SourceRequestResult, now time.Time) error {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return err
	}
	if input.Status < 0 || input.Status > 599 || (input.Status > 0 && input.Status < 100) || input.RetryAfterSeconds < 0 || input.RetryAfterSeconds > 86400 {
		return models.ErrEnrichmentInvalid
	}
	if _, err := s.CheckLease(ctx, lease, now); err != nil {
		return err
	}
	var row metadataRequestRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM metadata_request_permits WHERE job_uuid=? AND fence=? AND request_uuid=?", lease.JobUUID, lease.Fence, input.RequestUUID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrEnrichmentConflict
		}
		return err
	}
	if row.Reported {
		return nil
	}
	complete := sourcePacingAtomic(ctx)
	if _, err := dbWrapper.Exec(ctx, `UPDATE source_request_metrics SET responses=responses+1,rate_limited=rate_limited+?,errors=errors+?
 WHERE hour=? AND scope=? AND class='recovery' AND operation=?`, input.Status == 429, input.Status == 0 || input.Status >= 400, row.GrantedAt/3600000, row.Scope, row.Operation); err != nil {
		return err
	}
	if input.RetryAfterSeconds > 0 {
		if _, err := dbWrapper.Exec(ctx, "UPDATE source_request_budgets SET next_at_ms=max(next_at_ms,?) WHERE scope=?", now.Add(time.Duration(input.RetryAfterSeconds)*time.Second).UnixMilli(), row.Scope); err != nil {
			return err
		}
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE metadata_request_permits SET reported=1 WHERE job_uuid=?", lease.JobUUID); err != nil {
		return err
	}
	*complete = true
	return nil
}
