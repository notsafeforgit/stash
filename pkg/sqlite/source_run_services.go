package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/txn"
)

func (s *SourceRunStore) ReserveSource(ctx context.Context, lease models.SourceRunLease, url string, now time.Time) (*models.SourceRunServiceReservation, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	r, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	if _, _, err := sourceRunDefinition(ctx, r); err != nil {
		return nil, err
	}
	scope, err := scrape.SourceScopeV1(url)
	if err != nil {
		return nil, models.ErrSourceRunInvalid
	}
	origin, err := scrape.SourceOriginV1(url)
	if err != nil {
		return nil, models.ErrSourceRunInvalid
	}
	result := &models.SourceRunServiceReservation{RunUUID: r.UUID, Fence: r.Fence, Scope: scope}
	var held bool
	err = dbWrapper.Get(ctx, &held, "SELECT reserved FROM source_run_attempt_pacing WHERE run_uuid=? AND fence=? AND scope=?", r.UUID, r.Fence, scope)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		_, _, err := sourceRunDefinition(ctx, r)
		return err
	})
	if held {
		var cooling bool
		err = dbWrapper.Get(ctx, &cooling, "SELECT EXISTS(SELECT 1 FROM source_pacing WHERE scope=? AND (available_at_ms>? OR last_started_at_ms>?))", scope, now.UnixMilli(), now.UnixMilli())
		result.Ready = !cooling
		return result, err
	}
	if _, err := s.Recover(ctx, now, 100); err != nil {
		return nil, err
	}
	if _, err := (&EnrichmentJobStore{}).Maintain(ctx, now); err != nil {
		return nil, err
	}
	ready, err := sourcePacingReadyFor(ctx, scope, r.CollectionUUID, r.Operation == "enrich", "", r.UUID, now)
	if err != nil {
		return nil, err
	}
	complete := sourcePacingAtomic(ctx)
	if _, err := dbWrapper.Exec(ctx, "INSERT OR IGNORE INTO source_pacing(scope) VALUES(?)", scope); err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_run_attempt_pacing(run_uuid,fence,scope,reserved,source_origin) VALUES(?,?,?,0,?)
 ON CONFLICT(run_uuid,fence,scope) DO NOTHING`, r.UUID, r.Fence, scope, origin); err != nil {
		return nil, err
	}
	if ready {
		if _, err := dbWrapper.Exec(ctx, "UPDATE source_run_attempt_pacing SET reserved=1 WHERE run_uuid=? AND fence=? AND scope=? AND reserved=0", r.UUID, r.Fence, scope); err != nil {
			return nil, err
		}
		if err := sourcePacingStarted(ctx, scope, now); err != nil {
			return nil, err
		}
		if err := sourceTurnStarted(ctx, scope, r.Operation == "enrich", now); err != nil {
			return nil, err
		}
	}
	result.Ready = ready
	*complete = true
	return result, nil
}

func sourceRunPendingPacingReady(ctx context.Context, r *models.SourceRun, window string, now time.Time) (bool, error) {
	var scopes []string
	if err := dbWrapper.Select(ctx, &scopes, `SELECT p.scope FROM source_run_attempt_pacing p
 JOIN source_run_attempts a ON a.run_uuid=p.run_uuid AND a.fence=p.fence
 WHERE a.run_uuid=? AND a.fence=? AND a.window=? AND a.outcome IN ('retry','expired','deferred')`, r.UUID, r.Fence, window); err != nil {
		return false, err
	}
	for _, scope := range scopes {
		ready, err := sourcePacingReady(ctx, scope, r.CollectionUUID, r.Operation == "enrich", now)
		if err != nil || !ready {
			return false, err
		}
	}
	return true, nil
}

func validSourceServiceFailure(code string) bool {
	switch code {
	case "source_busy", "rate_limited", "timeout", "extraction_failed", "authentication", "access_denied", "challenge", "not_found":
		return true
	}
	return false
}

func sourceRunFailureScope(ctx context.Context, r *models.SourceRun, outcome models.SourceRunOutcome) (string, error) {
	scope := outcome.ErrorScope
	if scope == "" {
		return "", nil
	}
	if !validSourceServiceFailure(outcome.ErrorCode) || (outcome.State != "retry" && outcome.State != "deferred") {
		return "", models.ErrSourceRunInvalid
	}
	var held bool
	err := dbWrapper.Get(ctx, &held, "SELECT reserved FROM source_run_attempt_pacing WHERE run_uuid=? AND fence=? AND scope=?", r.UUID, r.Fence, scope)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !held && outcome.ErrorCode != "source_busy") {
		return "", models.ErrSourceRunInvalid
	}
	return scope, err
}
