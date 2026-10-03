package sqlite

import (
	"context"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const sourceDownloadTurn = 5 * time.Minute

// Only actual claim requests express interest. An unattended queue must not
// reserve a service indefinitely; blocked workers refresh this short-lived hint.
func sourceEnrichmentWaiting(ctx context.Context, job *models.ArchiveJob, now time.Time) error {
	if _, err := dbWrapper.Exec(ctx, "DELETE FROM source_enrichment_waiters WHERE expires_at_ms<=?", now.UnixMilli()); err != nil {
		return err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_enrichment_waiters(job_uuid,fence,first_requested_at_ms,refreshed_at_ms,expires_at_ms)
 VALUES(?,?,?,?,?) ON CONFLICT(job_uuid) DO UPDATE SET
 refreshed_at_ms=max(refreshed_at_ms,excluded.refreshed_at_ms),expires_at_ms=max(expires_at_ms,excluded.expires_at_ms)`,
		job.UUID, job.Fence, now.UnixMilli(), now.UnixMilli(), now.Add(90*time.Second).UnixMilli()); err != nil {
		return err
	}
	if _, err := dbWrapper.Exec(ctx, "DELETE FROM source_enrichment_waiter_scopes WHERE job_uuid=?", job.UUID); err != nil {
		return err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT OR IGNORE INTO source_pacing(scope)
 SELECT source_scope_v1(json_extract(p.value,'$.url')) FROM enrichment_checkpoints h,json_each(h.body,'$.pending') p WHERE h.job_uuid=?`, job.UUID); err != nil {
		return err
	}
	_, err := dbWrapper.Exec(ctx, `INSERT INTO source_enrichment_waiter_scopes(job_uuid,scope)
 SELECT job_uuid,scope FROM enrichment_job_pacing WHERE job_uuid=?
 UNION SELECT h.job_uuid,source_scope_v1(json_extract(p.value,'$.url'))
 FROM enrichment_checkpoints h,json_each(h.body,'$.pending') p WHERE h.job_uuid=?`, job.UUID, job.UUID)
	return err
}

// A turn becomes due after four download starts or two minutes of live waiting.
// The last enrichment start resets the age budget for other waiting jobs. Choose
// the oldest eligible requester; merely polling a newer job cannot steal its turn.
func sourceEnrichmentTurn(ctx context.Context, scope, collection string, now time.Time) (string, error) {
	var rows []archiveJobRow
	err := dbWrapper.Select(ctx, &rows, `SELECT j.* FROM source_enrichment_waiters w JOIN archive_jobs j ON j.uuid=w.job_uuid
 WHERE w.expires_at_ms>? AND w.refreshed_at_ms<=? AND j.state='queued' AND j.fence=w.fence
 AND j.available_at_ms<=? AND j.fence<j.max_attempts
 AND (json_extract(j.arguments,'$.collection_uuid')=? OR EXISTS(
  SELECT 1 FROM source_enrichment_waiter_scopes p WHERE p.job_uuid=w.job_uuid AND p.scope=?))
 AND NOT EXISTS(SELECT 1 FROM source_enrichment_waiter_scopes p JOIN source_pacing s ON s.scope=p.scope
  WHERE p.job_uuid=w.job_uuid AND (s.available_at_ms>? OR s.last_started_at_ms>?))
 AND ((SELECT max(s.download_starts) FROM source_enrichment_waiter_scopes p JOIN source_service_turns s ON s.scope=p.scope WHERE p.job_uuid=w.job_uuid)>=4
 OR max(w.first_requested_at_ms,coalesce((SELECT max(s.enrichment_started_at_ms) FROM source_enrichment_waiter_scopes p JOIN source_service_turns s ON s.scope=p.scope WHERE p.job_uuid=w.job_uuid),0))<=?)
 ORDER BY w.first_requested_at_ms,w.job_uuid LIMIT ?`, now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), collection, scope,
		now.UnixMilli(), now.UnixMilli(), now.Add(-2*time.Minute).UnixMilli(), archive.MaxEnrichmentJobs+1)
	if err != nil {
		return "", err
	}
	if len(rows) > archive.MaxEnrichmentJobs {
		return "", models.ErrSourcePayloadCorrupt
	}
	for _, row := range rows {
		job := row.resolve()
		work, err := archive.DecodeEnrichmentJob(job)
		if err != nil {
			return "", err
		}
		if _, err := enrichmentJobEligible(ctx, work, now); err != nil {
			if staleEnrichmentSource(err) {
				continue
			}
			return "", err
		}
		// A blocked linked metadata service must not reserve an unrelated parent.
		// Existing downloads are allowed to drain; existing enrichment is not
		// interrupted or bypassed by this scheduling hint.
		var busy bool
		err = dbWrapper.Get(ctx, &busy, `SELECT EXISTS(SELECT 1 FROM archive_jobs a INDEXED BY archive_jobs_active_work
 JOIN enrichment_attempt_pacing p ON p.job_uuid=a.uuid AND p.fence=a.fence
 WHERE a.kind='post.enrich' AND a.state IN ('queued','running') AND a.state='running'
 AND (json_extract(a.arguments,'$.collection_uuid')=? OR EXISTS(
  SELECT 1 FROM source_enrichment_waiter_scopes w WHERE w.job_uuid=? AND w.scope=p.scope)))
 OR EXISTS(SELECT 1 FROM source_runs r INDEXED BY source_runs_expired
 JOIN source_run_attempt_pacing p ON p.run_uuid=r.uuid AND p.fence=r.fence AND p.reserved=1
 WHERE r.state='running' AND r.operation='enrich' AND (r.collection_uuid=? OR EXISTS(
  SELECT 1 FROM source_enrichment_waiter_scopes w WHERE w.job_uuid=? AND w.scope=p.scope)))
 OR EXISTS(SELECT 1 FROM source_runs r JOIN source_run_cooldowns c ON c.target_key=r.target_key
 WHERE r.collection_uuid=? AND c.available_at_ms>?)`, work.CollectionUUID, job.UUID, work.CollectionUUID, job.UUID, work.CollectionUUID, now.UnixMilli())
		if err != nil {
			return "", err
		}
		if !busy {
			return job.UUID, nil
		}
	}
	return "", nil
}

func sourceTurnStarted(ctx context.Context, scope string, enrichment bool, now time.Time) error {
	if enrichment {
		_, err := dbWrapper.Exec(ctx, `UPDATE source_service_turns SET download_starts=0,enrichment_started_at_ms=max(enrichment_started_at_ms,?) WHERE scope=?`, now.UnixMilli(), scope)
		return err
	}
	_, err := dbWrapper.Exec(ctx, "UPDATE source_service_turns SET download_starts=min(4,download_starts+1) WHERE scope=?", scope)
	return err
}

func sourceAttemptTurnStarted(ctx context.Context, id string, fence int64, enrichment bool, sourceRun bool, now time.Time) error {
	query := "SELECT scope FROM enrichment_attempt_pacing WHERE job_uuid=? AND fence=?"
	if sourceRun {
		query = "SELECT scope FROM source_run_attempt_pacing WHERE run_uuid=? AND fence=? AND reserved=1"
	}
	var scopes []string
	if err := dbWrapper.Select(ctx, &scopes, query, id, fence); err != nil {
		return err
	}
	for _, scope := range scopes {
		if err := sourcePacingStarted(ctx, scope, now); err != nil {
			return err
		}
		if err := sourceTurnStarted(ctx, scope, enrichment, now); err != nil {
			return err
		}
	}
	return nil
}
