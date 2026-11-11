package sqlite

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

// Only the standard author-search pass belongs to this monitoring policy.
// Saved feeds, arbitrary searches, individual posts and other sites do not.
func redditMonitoringProfile(r *models.SourceRun) string {
	if r.Operation != "download" || r.AttachmentRetry || r.RootUUID == nil {
		return ""
	}
	u, err := url.Parse(r.TargetURL)
	if err != nil || strings.Trim(u.Path, "/") != "search" {
		return ""
	}
	return scrape.RedditProfileURL(r.TargetURL)
}

// Reuse durable profile evidence. An author-search success is not evidence that
// the profile recovered; neither are HTTP 403/429 or failures on linked hosts.
// Histories include consolidated runs and use observation times, not the time a
// scheduler last widened their pending window.
func redditMonitoringPaused(ctx context.Context, r *models.SourceRun) (bool, error) {
	profile := redditMonitoringProfile(r)
	if profile == "" {
		return false, nil
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, r.CollectionUUID)
	if err != nil {
		return false, err
	}
	if collection == nil || collection.Namespace != "native:reddit" || collection.Kind != "account" || scrape.RedditProfileURL(collection.TargetURL) != profile {
		return false, nil
	}
	handle := strings.TrimSuffix(strings.TrimPrefix(profile, "https://www.reddit.com/user/"), "/")
	history, err := (&SourceBackfillStore{}).Status(ctx, models.BackfillSubject{RootUUID: *r.RootUUID, Platform: "reddit", Account: handle}, "reddit-new")
	if err != nil || !history.AccountComplete {
		return false, err
	}
	type observation struct {
		URL     string `db:"url"`
		Outcome string `db:"outcome"`
		Started int64  `db:"started_at_ms"`
	}
	var observations []observation
	err = dbWrapper.Select(ctx, &observations, `SELECT coalesce(v.url,c.target_url) AS url,a.outcome,a.started_at_ms
 FROM source_runs r JOIN source_run_attempts a ON a.run_uuid=r.uuid
 JOIN source_collection_revisions c ON c.collection_uuid=r.collection_uuid AND c.revision=r.collection_revision
 LEFT JOIN source_run_retrievals v ON v.run_uuid=r.uuid
 LEFT JOIN source_run_attempt_failures f ON f.run_uuid=a.run_uuid AND f.fence=a.fence
 WHERE r.collection_uuid=? AND r.operation='download' AND c.namespace='native:reddit'
 AND a.ended_at_ms IS NOT NULL AND coalesce(v.url,c.target_url) NOT LIKE '%/search%'
 AND NOT EXISTS(SELECT 1 FROM source_run_attachment_retries x WHERE x.run_uuid=r.uuid)
 AND (a.outcome='succeeded' OR (f.scope='service:reddit' AND f.error_code IN ('not_found','account_not_found','account_suspended')))
 ORDER BY a.ended_at_ms DESC,a.started_at_ms DESC`, r.CollectionUUID)
	if err != nil {
		return false, err
	}
	for _, o := range observations {
		if scrape.RedditProfileURL(o.URL) != profile {
			continue
		}
		if o.Outcome == "succeeded" {
			return false, nil
		}
		// Profile observations arrive before listing/download completion. A healthy
		// about response in this attempt also prevents a later listing 404 from
		// being mistaken for a missing account. Account index bounds the common path.
		query := `SELECT observed_at FROM source_account_profiles WHERE collection_uuid=? AND review='' AND json_extract(source,'$.user.is_suspended') IS NOT 1`
		args := []any{r.CollectionUUID}
		if collection.AccountUUID != nil {
			query += ` AND account_uuid=?`
			args = append(args, *collection.AccountUUID)
		}
		var healthy []time.Time
		if err := dbWrapper.Select(ctx, &healthy, query, args...); err != nil {
			return false, err
		}
		for _, observed := range healthy {
			if observed.UnixMilli() >= o.Started {
				return false, nil
			}
		}
		return true, nil
	}
	return false, nil
}

// Recovery wakes retained searches without manufacturing completed coverage.
// Only queued monitoring pauses are touched; manual review, cancellations and
// initial/recovery work retain their own lifecycle.
func resumeRedditMonitoring(ctx context.Context, collection string, now time.Time) error {
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, `SELECT uuid FROM source_runs WHERE collection_uuid=? AND state='queued' AND error_code='account_check_pending'`, collection); err != nil {
		return err
	}
	for _, id := range ids {
		run, err := (&SourceRunStore{}).Find(ctx, id)
		if err != nil {
			return err
		}
		paused, err := redditMonitoringPaused(ctx, run)
		if err != nil {
			return err
		}
		if paused {
			continue
		}
		if _, err := dbWrapper.Exec(ctx, `UPDATE source_runs SET available_at_ms=min(available_at_ms,?),revision=revision+1,updated_at_ms=? WHERE uuid=?`, now.UnixMilli(), now.UnixMilli(), id); err != nil {
			return err
		}
	}
	return nil
}
