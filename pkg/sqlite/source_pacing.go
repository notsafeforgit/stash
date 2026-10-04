package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/txn"
)

func sourcePacingAtomic(ctx context.Context) *bool {
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrSourcePacingAtomic
		}
		return nil
	})
	return &complete
}

func sourcePacingScope(ctx context.Context, id string, enrichment bool) (string, error) {
	query := "SELECT scope FROM source_run_pacing WHERE run_uuid=?"
	if enrichment {
		query = "SELECT scope FROM enrichment_job_pacing WHERE job_uuid=?"
	}
	var scope string
	if err := dbWrapper.Get(ctx, &scope, query, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", models.ErrSourcePayloadCorrupt
		}
		return "", err
	}
	return scope, nil
}

// Both claim paths call this inside their managed write transaction. Existing
// download/destination exclusions still apply; independent downloads may run
// together, but metadata enrichment cannot compete with them on the same service.
func sourcePacingReady(ctx context.Context, scope, collection string, enrichment bool, now time.Time) (bool, error) {
	return sourcePacingReadyFor(ctx, scope, collection, enrichment, "", "", now)
}

func sourcePacingReadyFor(ctx context.Context, scope, collection string, enrichment bool, ignoreJob, ignoreRun string, now time.Time) (bool, error) {
	var busy bool
	if err := dbWrapper.Get(ctx, &busy, `SELECT
 EXISTS(SELECT 1 FROM source_pacing WHERE scope=? AND (available_at_ms>? OR last_started_at_ms>?))
 OR EXISTS(SELECT 1 FROM archive_jobs j INDEXED BY archive_jobs_active_work
  JOIN enrichment_attempt_pacing p ON p.job_uuid=j.uuid AND p.fence=j.fence
  WHERE j.kind IN ('post.enrich','account.list_page','post.verify_candidate') AND j.state IN ('queued','running') AND j.state='running'
   AND j.uuid!=? AND (p.scope=? OR json_extract(j.arguments,'$.collection_uuid')=?))
 OR EXISTS(SELECT 1 FROM source_runs r INDEXED BY source_runs_expired
  JOIN source_run_attempt_pacing p ON p.run_uuid=r.uuid AND p.fence=r.fence AND p.reserved=1
  WHERE r.state='running' AND r.uuid!=? AND (p.scope=? OR r.collection_uuid=?) AND (? OR r.operation='enrich'))`,
		scope, now.UnixMilli(), now.UnixMilli(), ignoreJob, scope, collection, ignoreRun, scope, collection, enrichment); err != nil {
		return false, err
	}
	if busy {
		return !busy, nil
	}
	turn, err := sourceEnrichmentTurn(ctx, scope, collection, now)
	if err != nil {
		return false, err
	}
	if turn != "" {
		return enrichment && turn == ignoreJob, nil
	}
	if !enrichment {
		return true, nil
	}
	// Current, enabled download definitions take precedence. Stale definitions,
	// deferred runs and retries still in backoff cannot indefinitely reserve it.
	if err := dbWrapper.Get(ctx, &busy, `SELECT EXISTS(
 SELECT 1 FROM source_runs r INDEXED BY source_runs_active
 JOIN source_run_pacing p ON p.run_uuid=r.uuid
 JOIN source_collections c ON c.uuid=r.collection_uuid AND c.revision=r.collection_revision
 JOIN source_collection_revisions d ON d.collection_uuid=c.uuid AND d.revision=c.revision
 JOIN media_roots m ON m.uuid=r.root_uuid AND m.revision=r.root_revision
 JOIN media_root_revisions v ON v.root_uuid=m.uuid AND v.revision=m.revision
 WHERE r.state IN ('queued','running','deferred') AND r.state='queued' AND r.operation='download'
  AND r.available_at_ms<=? AND (p.scope=? OR r.collection_uuid=? OR EXISTS(
   SELECT 1 FROM source_run_attempt_pacing s JOIN source_run_attempts a ON a.run_uuid=s.run_uuid AND a.fence=s.fence
   WHERE s.run_uuid=r.uuid AND s.fence=r.fence AND s.scope=? AND a.outcome IN ('retry','expired','deferred')
    AND a.window=json_extract(r.pending,'$[#-1]'))) AND d.state='active' AND v.state='active'
  AND NOT EXISTS(SELECT 1 FROM source_pacing s WHERE s.scope=p.scope AND (s.available_at_ms>? OR s.last_started_at_ms>?))
  AND NOT EXISTS(SELECT 1 FROM source_run_attempt_pacing a
   JOIN source_run_attempts b ON b.run_uuid=a.run_uuid AND b.fence=a.fence JOIN source_pacing s ON s.scope=a.scope
   WHERE a.run_uuid=r.uuid AND a.fence=r.fence AND b.outcome IN ('retry','expired','deferred')
    AND b.window=json_extract(r.pending,'$[#-1]') AND (s.available_at_ms>? OR s.last_started_at_ms>?))
  AND NOT EXISTS(SELECT 1 FROM source_run_cooldowns d WHERE d.target_key=r.target_key AND d.available_at_ms>?)
 ) OR EXISTS(SELECT 1 FROM source_runs r JOIN source_run_cooldowns d ON d.target_key=r.target_key
 WHERE r.collection_uuid=? AND d.available_at_ms>?)`,
		now.UnixMilli(), scope, collection, scope, now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), collection, now.UnixMilli()); err != nil {
		return false, err
	}
	return !busy, nil
}

// ReserveSource runs before initializing a newly discovered child extractor.
// Reservations share the current attempt's lifetime; expired/finished attempts
// remain historical evidence but cannot hold a service. No website URL or
// credential is persisted by this operation.
func (s *EnrichmentJobStore) ReserveSource(ctx context.Context, lease models.EnrichmentJobLease, rawURL string, now time.Time) (bool, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return false, err
	}
	job, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return false, err
	}
	work, err := archive.DecodeEnrichmentJob(job)
	if err != nil {
		return false, err
	}
	return reserveMetadataSource(ctx, job, work.CollectionUUID, rawURL, now)
}

func reserveMetadataSource(ctx context.Context, job *models.ArchiveJob, collection, rawURL string, now time.Time) (bool, error) {
	scope, err := scrape.SourceScopeV1(rawURL)
	if err != nil {
		return false, models.ErrEnrichmentInvalid
	}
	rootScope, err := sourcePacingScope(ctx, job.UUID, true)
	if err != nil {
		return false, err
	}
	// Match the reviewed metadata worker's bounded child extractor set. Source
	// pacing is not permission to reserve unrelated websites or arbitrary hosts.
	if scope != rootScope && scope != "service:redgifs" && scope != "service:imgur" {
		return false, models.ErrEnrichmentInvalid
	}
	var held, cooling bool
	if err := dbWrapper.Get(ctx, &held, "SELECT EXISTS(SELECT 1 FROM enrichment_attempt_pacing WHERE job_uuid=? AND fence=? AND scope=?)", job.UUID, job.Fence, scope); err != nil {
		return false, err
	}
	if err := dbWrapper.Get(ctx, &cooling, "SELECT EXISTS(SELECT 1 FROM source_pacing WHERE scope=? AND (available_at_ms>? OR last_started_at_ms>?))", scope, now.UnixMilli(), now.UnixMilli()); err != nil {
		return false, err
	}
	if held || cooling {
		return held && !cooling, nil
	}
	if _, err := (&SourceRunStore{}).Recover(ctx, now, 100); err != nil {
		return false, err
	}
	if _, err := (&EnrichmentJobStore{}).Maintain(ctx, now); err != nil {
		return false, err
	}
	ready, err := sourcePacingReadyFor(ctx, scope, collection, true, job.UUID, "", now)
	if err != nil || !ready {
		return false, err
	}
	complete := sourcePacingAtomic(ctx)
	if _, err := dbWrapper.Exec(ctx, "INSERT OR IGNORE INTO source_pacing(scope) VALUES(?)", scope); err != nil {
		return false, err
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO enrichment_attempt_pacing(job_uuid,fence,scope) VALUES(?,?,?)", job.UUID, job.Fence, scope); err != nil {
		return false, err
	}
	if err := sourcePacingStarted(ctx, scope, now); err != nil {
		return false, err
	}
	if err := sourceTurnStarted(ctx, scope, true, now); err != nil {
		return false, err
	}
	*complete = true
	return true, nil
}

func sourcePacingStarted(ctx context.Context, scope string, now time.Time) error {
	_, err := dbWrapper.Exec(ctx, "UPDATE source_pacing SET last_started_at_ms=max(last_started_at_ms,?) WHERE scope=?", now.UnixMilli(), scope)
	return err
}

func enrichmentPendingPacingReady(ctx context.Context, job, collection, rootScope string, now time.Time) (bool, error) {
	var scopes []string
	if err := dbWrapper.Select(ctx, &scopes, `SELECT DISTINCT source_scope_v1(json_extract(p.value,'$.url'))
 FROM (SELECT job_uuid,body FROM enrichment_checkpoints UNION ALL SELECT job_uuid,body FROM discovery_detail_checkpoints) h,json_each(h.body,'$.pending') p WHERE h.job_uuid=?
 UNION SELECT scope FROM enrichment_job_seed_services WHERE job_uuid=?
 AND NOT EXISTS(SELECT 1 FROM enrichment_checkpoints WHERE job_uuid=?)`, job, job, job); err != nil {
		return false, err
	}
	for _, scope := range scopes {
		if scope == rootScope {
			continue
		}
		ready, err := sourcePacingReadyFor(ctx, scope, collection, true, job, "", now)
		if err != nil || !ready {
			return false, err
		}
	}
	return true, nil
}

func sourcePacingDelay(code string, retry time.Time, now time.Time) time.Duration {
	var delay time.Duration
	switch code {
	case "rate_limited", "timeout", "extraction_failed":
		delay = time.Hour
	case "authentication", "challenge":
		delay = 24 * time.Hour
	default:
		return 0 // Missing posts, denied accounts and local worker failures are not service outages.
	}
	return max(delay, retry.Sub(now))
}

func sourcePacingPause(ctx context.Context, scope, code string, until time.Time) error {
	_, err := dbWrapper.Exec(ctx, `INSERT INTO source_pacing(scope,available_at_ms,reason) VALUES(?,?,?)
 ON CONFLICT(scope) DO UPDATE SET
 reason=CASE WHEN excluded.available_at_ms>available_at_ms THEN excluded.reason ELSE reason END,
 available_at_ms=max(available_at_ms,excluded.available_at_ms)`, scope, until.UnixMilli(), code)
	return err
}

// A retained child failure belongs to that child's service, not to the parent
// post's website. The target job still waits until its required child can retry.
func sourcePacingEnrichmentFailure(ctx context.Context, job *models.ArchiveJob, code string, retry time.Time, now time.Time) (time.Time, error) {
	delay := sourcePacingDelay(code, retry, now)
	if delay == 0 {
		return retry, nil
	}
	scopes := map[string]bool{}
	var pendingJSON string
	err := dbWrapper.Get(ctx, &pendingJSON, "SELECT json_extract(body,'$.pending') FROM enrichment_checkpoints WHERE job_uuid=? UNION ALL SELECT json_extract(body,'$.pending') FROM discovery_detail_checkpoints WHERE job_uuid=?", job.UUID, job.UUID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}
	if err == nil {
		var pending []models.EnrichmentReference
		if err := json.Unmarshal([]byte(pendingJSON), &pending); err != nil {
			return time.Time{}, err
		}
		for _, ref := range pending {
			if ref.Reason == code {
				scope, err := scrape.SourceScopeV1(ref.URL)
				if err != nil {
					return time.Time{}, err
				}
				scopes[scope] = true
			}
		}
	}
	if len(scopes) == 0 {
		if job.Kind == models.ArchiveJobEnrichPost {
			work, err := archive.DecodeEnrichmentJob(job)
			if err != nil {
				return time.Time{}, err
			}
			// A seeded job never refetches the parent service.
			if work.Handoff != nil {
				return maxTime(retry, now.Add(delay)), nil
			}
		}
		scope, err := sourcePacingScope(ctx, job.UUID, true)
		if err != nil {
			return time.Time{}, err
		}
		scopes[scope] = true
	}
	until := now.Add(delay)
	for scope := range scopes {
		if err := sourcePacingPause(ctx, scope, code, until); err != nil {
			return time.Time{}, err
		}
	}
	return maxTime(retry, until), nil
}

func sourcePacingMetadataFailure(ctx context.Context, job *models.ArchiveJob, code string, retry time.Time, now time.Time) (time.Time, error) {
	if job.Kind == models.ArchiveJobEnrichPost || job.Kind == models.ArchiveJobVerifyCandidate {
		return sourcePacingEnrichmentFailure(ctx, job, code, retry, now)
	}
	delay := sourcePacingDelay(code, retry, now)
	if delay == 0 {
		return retry, nil
	}
	scope, err := sourcePacingScope(ctx, job.UUID, true)
	if err != nil {
		return time.Time{}, err
	}
	until := now.Add(delay)
	if err := sourcePacingPause(ctx, scope, code, until); err != nil {
		return time.Time{}, err
	}
	return maxTime(retry, until), nil
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
