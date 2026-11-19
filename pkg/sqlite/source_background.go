package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

// Background metadata uses spare request capacity beside a running download.
// A worker opts in per claim. The claim registers no turn interest and starts
// no turn, downloads do not wait for it, and each of its requests waits while a
// download or a turn-holding metadata job is waiting for the same service. The
// shared request spacing, Retry-After budget and cooldowns still apply.

const backgroundAttemptJoin = "LEFT JOIN enrichment_background_attempts b ON b.job_uuid=j.uuid AND b.fence=j.fence"

func (s *EnrichmentJobStore) ClaimBackground(ctx context.Context, id string, revision int64, owner string, now time.Time, duration time.Duration) (*models.ArchiveJob, error) {
	return (&ArchiveJobStore{}).claimByID(ctx, id, revision, owner, now, duration, true)
}

func backgroundMetadataAttempt(ctx context.Context, job string) (bool, error) {
	var background bool
	err := dbWrapper.Get(ctx, &background, `SELECT EXISTS(SELECT 1 FROM archive_jobs j
 JOIN enrichment_background_attempts b ON b.job_uuid=j.uuid AND b.fence=j.fence WHERE j.uuid=? AND j.state='running')`, job)
	return background, err
}

// sourceBackgroundReady keeps cooldowns, metadata runs and the collection's own
// metadata exclusion. Running and queued downloads do not block it.
func sourceBackgroundReady(ctx context.Context, scope, collection, job string, now time.Time) (bool, error) {
	var busy bool
	err := dbWrapper.Get(ctx, &busy, `SELECT
 EXISTS(SELECT 1 FROM source_pacing WHERE scope=? AND (available_at_ms>? OR last_started_at_ms>?))
 OR EXISTS(SELECT 1 FROM archive_jobs j INDEXED BY archive_jobs_active_work
  WHERE j.kind='post.enrich' AND j.state IN ('queued','running') AND j.state='running'
   AND j.uuid!=? AND json_extract(j.arguments,'$.collection_uuid')=?)
 OR EXISTS(SELECT 1 FROM source_runs r INDEXED BY source_runs_expired
  JOIN source_run_attempt_pacing p ON p.run_uuid=r.uuid AND p.fence=r.fence AND p.reserved=1
  WHERE r.state='running' AND r.operation='enrich' AND (p.scope=? OR r.collection_uuid=?))
 OR EXISTS(SELECT 1 FROM source_runs r JOIN source_run_cooldowns d ON d.target_key=r.target_key
  WHERE r.collection_uuid=? AND d.available_at_ms>?)`,
		scope, now.UnixMilli(), now.UnixMilli(), job, collection, scope, collection, collection, now.UnixMilli())
	return !busy, err
}

// A background claim starts only beside a download that is already running on
// the service, never beside another metadata job there or a running download
// of the same collection. Retained child services must also be available.
func sourceBackgroundClaimReady(ctx context.Context, scope, collection, job string, now time.Time) (bool, error) {
	ready, err := sourceBackgroundReady(ctx, scope, collection, job, now)
	if err != nil || !ready {
		return false, err
	}
	var eligible bool
	if err := dbWrapper.Get(ctx, &eligible, `SELECT
 EXISTS(SELECT 1 FROM source_runs r INDEXED BY source_runs_expired
  JOIN source_run_attempt_pacing p ON p.run_uuid=r.uuid AND p.fence=r.fence AND p.reserved=1
  WHERE r.state='running' AND r.operation='download' AND p.scope=?)
 AND NOT EXISTS(SELECT 1 FROM source_runs r INDEXED BY source_runs_expired WHERE r.state='running' AND r.collection_uuid=?)
 AND NOT EXISTS(SELECT 1 FROM archive_jobs j INDEXED BY archive_jobs_active_work
  JOIN enrichment_attempt_pacing p ON p.job_uuid=j.uuid AND p.fence=j.fence
  WHERE j.kind='post.enrich' AND j.state IN ('queued','running') AND j.state='running' AND j.uuid!=? AND p.scope=?)`,
		scope, collection, job, scope); err != nil || !eligible {
		return false, err
	}
	var scopes []string
	if err := dbWrapper.Select(ctx, &scopes, `SELECT DISTINCT source_scope_v1(json_extract(p.value,'$.url'))
 FROM enrichment_checkpoints h,json_each(h.body,'$.pending') p WHERE h.job_uuid=?`, job); err != nil {
		return false, err
	}
	for _, child := range scopes {
		if child == scope {
			continue
		}
		ready, err := sourceBackgroundReady(ctx, child, collection, job, now)
		if err != nil || !ready {
			return false, err
		}
	}
	return true, nil
}

func markBackgroundMetadataAttempt(ctx context.Context, job string, fence int64, now time.Time) error {
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO enrichment_background_attempts(job_uuid,fence) VALUES(?,?)", job, fence); err != nil {
		return err
	}
	var scopes []string
	if err := dbWrapper.Select(ctx, &scopes, "SELECT scope FROM enrichment_attempt_pacing WHERE job_uuid=? AND fence=?", job, fence); err != nil {
		return err
	}
	for _, scope := range scopes {
		if err := sourcePacingStarted(ctx, scope, now); err != nil {
			return err
		}
	}
	return nil
}

// sourceBackgroundRequestWait returns whether a background request must wait
// for a preferred requester. Within its first metadata turn length the job keeps
// polling; after that it yields (zero wait) so the job returns to the queue.
func sourceBackgroundRequestWait(ctx context.Context, scope string, job *models.ArchiveJob, now time.Time) (int64, bool, error) {
	stamp := now.UnixMilli()
	var preferred bool
	if err := dbWrapper.Get(ctx, &preferred, `SELECT
 EXISTS(SELECT 1 FROM source_request_permits p JOIN source_runs r ON r.uuid=p.run_uuid AND r.fence=p.fence
  WHERE p.scope=? AND p.granted_at_ms IS NULL AND p.touched_at_ms>? AND r.state='running' AND r.lease_until_ms>?)
 OR EXISTS(SELECT 1 FROM archive_jobs j INDEXED BY archive_jobs_active_work
  JOIN enrichment_attempt_pacing p ON p.job_uuid=j.uuid AND p.fence=j.fence `+backgroundAttemptJoin+`
  WHERE j.kind='post.enrich' AND j.state IN ('queued','running') AND j.state='running'
   AND j.uuid!=? AND p.scope=? AND b.job_uuid IS NULL)`, scope, stamp-60000, stamp, job.UUID, scope); err != nil || !preferred {
		return 0, false, err
	}
	var started int64
	if err := dbWrapper.Get(ctx, &started, "SELECT started_at_ms FROM archive_job_attempts WHERE job_uuid=? AND fence=?", job.UUID, job.Fence); err != nil {
		return 0, false, err
	}
	if stamp-started >= sourceMetadataTurn.Milliseconds() {
		return 0, true, nil
	}
	return 5000, true, nil
}

func validateBackgroundMetadataSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"enrichment_background_attempts", "enrichment_background_attempt_current", "enrichment_background_attempt_immutable"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	if !auditData {
		return nil
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM enrichment_background_attempts b
 WHERE NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN archive_job_attempts a ON a.job_uuid=j.uuid
  WHERE j.uuid=b.job_uuid AND a.fence=b.fence AND j.kind='post.enrich'))`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}
