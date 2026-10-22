package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// A probe is a normal queued HTTP request. A lost worker cannot reserve it
// indefinitely, and an old response cannot release a newer outage.
const sourceProbeLease = 2 * time.Minute

type sourceBackoff struct {
	Step       int    `db:"backoff_step"`
	At         int64  `db:"backoff_at_ms"`
	Until      int64  `db:"blocked_until_ms"`
	Probe      string `db:"probe_uuid"`
	ProbeUntil int64  `db:"probe_until_ms"`
	Owner      string `db:"probe_owner"`
}

func sourceRequestCoolingWait(ctx context.Context, scope string, now time.Time) (int64, error) {
	var until int64
	err := dbWrapper.Get(ctx, &until, "SELECT coalesce((SELECT blocked_until_ms FROM source_request_budgets WHERE scope=?),0)", scope)
	return min(30000, max(0, until-now.UnixMilli())), err
}

func sourceRequestOwner(kind, id string, fence int64) string {
	return fmt.Sprintf("%s:%s:%d", kind, id, fence)
}

func sourceRequestWait(ctx context.Context, scope, request, owner string, now time.Time) (int64, error) {
	var until int64
	err := dbWrapper.Get(ctx, &until, `SELECT max(next_at_ms,blocked_until_ms,
 CASE WHEN probe_uuid!=? OR probe_owner!=? THEN probe_until_ms ELSE 0 END) FROM source_request_budgets WHERE scope=?`, request, owner, scope)
	return min(30000, max(0, until-now.UnixMilli())), err
}

func sourceRequestGranted(ctx context.Context, scope, request, owner string, now time.Time) (bool, error) {
	var probe bool
	if err := dbWrapper.Get(ctx, &probe, "SELECT backoff_step>0 FROM source_request_budgets WHERE scope=?", scope); err != nil {
		return false, err
	}
	_, err := dbWrapper.Exec(ctx, `UPDATE source_request_budgets SET next_at_ms=?,
 probe_uuid=CASE WHEN backoff_step>0 THEN ? ELSE '' END,
 probe_owner=CASE WHEN backoff_step>0 THEN ? ELSE '' END,
 probe_until_ms=CASE WHEN backoff_step>0 THEN ? ELSE 0 END WHERE scope=?`,
		now.UnixMilli()+requestInterval(scope), request, owner, now.Add(sourceProbeLease).UnixMilli(), scope)
	return probe, err
}

func validSourceRequestResult(input models.SourceRequestResult) bool {
	return input.Status >= 0 && input.Status <= 599 && (input.Status == 0 || input.Status >= 100) &&
		input.RetryAfterSeconds >= 0 && input.RetryAfterSeconds <= 86400 &&
		(input.BlockReason == "" || (input.BlockReason == "network_security" && (input.Status == 200 || input.Status == 403)))
}

func sourceRequestFeedback(ctx context.Context, scope, owner string, granted int64, probe bool, input models.SourceRequestResult, now time.Time) (bool, error) {
	var b sourceBackoff
	if err := dbWrapper.Get(ctx, &b, `SELECT backoff_step,backoff_at_ms,blocked_until_ms,probe_uuid,probe_until_ms,probe_owner
 FROM source_request_budgets WHERE scope=?`, scope); err != nil {
		return false, err
	}
	blocked := input.Status == 429 || input.BlockReason != ""
	controlled := probe || blocked
	if granted < b.At || (probe && (b.Probe != input.RequestUUID || b.Owner != owner)) {
		return controlled, nil // Late responses cannot change newer observations.
	}
	if input.RetryAfterSeconds > 0 {
		if _, err := dbWrapper.Exec(ctx, "UPDATE source_request_budgets SET next_at_ms=max(next_at_ms,?) WHERE scope=?",
			now.Add(time.Duration(input.RetryAfterSeconds)*time.Second).UnixMilli(), scope); err != nil {
			return false, err
		}
	}
	if blocked || (probe && (input.Status == 0 || input.Status >= 500)) {
		step := min(5, b.Step+1)
		delay := min(15*time.Minute, time.Minute*time.Duration(1<<(step-1)))
		if input.RetryAfterSeconds > 0 {
			delay = max(time.Duration(requestInterval(scope))*time.Millisecond, time.Duration(input.RetryAfterSeconds)*time.Second)
		}
		until := max(b.Until, now.Add(delay).UnixMilli())
		if _, err := dbWrapper.Exec(ctx, `UPDATE source_request_budgets SET backoff_step=?,backoff_at_ms=?,
 blocked_until_ms=?,next_at_ms=max(next_at_ms,?),probe_uuid='',probe_owner='',probe_until_ms=0 WHERE scope=?`, step, now.UnixMilli(), until, until, scope); err != nil {
			return false, err
		}
		return true, sourcePacingPause(ctx, scope, "rate_limited", time.UnixMilli(until))
	}
	if probe {
		// A normal response, including an account-specific 404/403, proves the
		// service is reachable. Its ordinary account error still applies.
		_, err := dbWrapper.Exec(ctx, `UPDATE source_request_budgets SET backoff_step=0,backoff_at_ms=?,
 blocked_until_ms=0,probe_uuid='',probe_owner='',probe_until_ms=0 WHERE scope=?`, now.UnixMilli(), scope)
		return false, err
	}
	return false, nil
}

func sourceAttemptBackoff(ctx context.Context, id string, fence int64, metadata bool) (bool, error) {
	query := "SELECT EXISTS(SELECT 1 FROM source_request_permits WHERE run_uuid=? AND fence=? AND reported=1 AND backoff=1)"
	if metadata {
		query = "SELECT EXISTS(SELECT 1 FROM metadata_request_permits WHERE job_uuid=? AND fence=? AND reported=1 AND backoff=1)"
	}
	var backoff bool
	err := dbWrapper.Get(ctx, &backoff, query, id, fence)
	return backoff, err
}

func (s *EnrichmentJobStore) RequestBackoff(ctx context.Context, lease models.EnrichmentJobLease, now time.Time) (bool, error) {
	if _, err := s.CheckLease(ctx, lease, now); err != nil {
		return false, err
	}
	return sourceAttemptBackoff(ctx, lease.JobUUID, lease.Fence, true)
}
