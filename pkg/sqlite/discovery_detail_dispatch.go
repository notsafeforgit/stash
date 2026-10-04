package sqlite

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// Collections inspects only the bounded active detail jobs. It never scans
// catalog history or interprets a candidate URL as an accepted association.
func (s *DiscoveryDetailStore) Collections(ctx context.Context, q models.EnrichmentCollectionQuery, now time.Time) ([]models.EnrichmentCollectionCandidate, error) {
	if len(q.Scopes)+len(q.Roots) < 1 || len(q.Scopes)+len(q.Roots) > 128 || !archive.ValidSHA256(q.PolicySHA256) ||
		q.ExtractorVersion == "" || len(q.ExtractorVersion) > 128 || !utf8.ValidString(q.ExtractorVersion) || strings.ContainsAny(q.ExtractorVersion, "\r\n\x00") ||
		(q.After != "" && !validSourceRunUUID(q.After)) || !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(q.Limit)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	for _, scope := range q.Scopes {
		if !validSourceRunUUID(scope.CollectionUUID) || (scope.RootUUID != nil && !validSourceRunUUID(*scope.RootUUID)) {
			return nil, models.ErrDiscoveryInvalid
		}
	}
	for _, root := range q.Roots {
		if !validSourceRunUUID(root) {
			return nil, models.ErrDiscoveryInvalid
		}
	}
	rows, err := activeDiscoveryDetails(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	ret := []models.EnrichmentCollectionCandidate{}
	for _, row := range rows {
		job := row.resolve()
		if job.State != "queued" || now.Before(job.AvailableAt) {
			continue
		}
		work, err := archive.DecodeDiscoveryDetailJob(job)
		if err != nil {
			return nil, err
		}
		if seen[work.CollectionUUID] || work.CollectionUUID <= q.After || work.PolicySHA256 != q.PolicySHA256 || work.ExtractorVersion != q.ExtractorVersion {
			continue
		}
		allowed := false
		for _, scope := range q.Scopes {
			allowed = allowed || (scope.CollectionUUID == work.CollectionUUID && reflect.DeepEqual(scope.RootUUID, work.RootUUID))
		}
		for _, root := range q.Roots {
			allowed = allowed || (work.RootUUID != nil && root == *work.RootUUID)
		}
		if !allowed {
			continue
		}
		if err := discoveryDetailEligible(ctx, work, now); err != nil {
			if errors.Is(err, models.ErrDiscoveryConflict) {
				continue
			}
			return nil, err
		}
		seen[work.CollectionUUID] = true
		ret = append(ret, models.EnrichmentCollectionCandidate{UUID: work.CollectionUUID})
	}
	sort.Slice(ret, func(i, j int) bool { return ret[i].UUID < ret[j].UUID })
	if len(ret) > limit {
		ret = ret[:limit]
	}
	return ret, nil
}

func activeDiscoveryDetails(ctx context.Context) ([]archiveJobRow, error) {
	var rows []archiveJobRow
	err := dbWrapper.Select(ctx, &rows, `SELECT * FROM archive_jobs INDEXED BY archive_jobs_active_work
 WHERE kind='post.verify_candidate' AND state IN ('queued','running') ORDER BY id LIMIT ?`, archive.MaxDiscoveryDetailJobs+1)
	if err != nil {
		return nil, err
	}
	if len(rows) > archive.MaxDiscoveryDetailJobs {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return rows, nil
}
func (s *DiscoveryDetailStore) Ready(ctx context.Context, collection, policy, extractor string, after int64, limit int, now time.Time) ([]models.DiscoveryJobCandidate, error) {
	if !validDiscoveryProfileRequest(collection, policy, extractor, now) || after < 0 {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	rows, err := activeDiscoveryDetails(ctx)
	if err != nil {
		return nil, err
	}
	ret := []models.DiscoveryJobCandidate{}
	for _, row := range rows {
		job := row.resolve()
		if job.Sequence <= after || job.State != "queued" || now.Before(job.AvailableAt) {
			continue
		}
		work, err := archive.DecodeDiscoveryDetailJob(job)
		if err != nil {
			return nil, err
		}
		if work.CollectionUUID != collection || work.PolicySHA256 != policy || work.ExtractorVersion != extractor {
			continue
		}
		if err := discoveryDetailEligible(ctx, work, now); err != nil {
			if errors.Is(err, models.ErrDiscoveryConflict) {
				continue
			}
			return nil, err
		}
		ret = append(ret, models.DiscoveryJobCandidate{Sequence: job.Sequence, UUID: job.UUID})
		if len(ret) == limit {
			break
		}
	}
	return ret, nil
}
func (s *DiscoveryDetailStore) Maintain(ctx context.Context, now time.Time) (*models.DiscoveryMaintenanceResult, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	complete := discoveryAtomic(ctx)
	rows, err := activeDiscoveryDetails(ctx)
	if err != nil {
		return nil, err
	}
	ret := &models.DiscoveryMaintenanceResult{}
	for _, row := range rows {
		job := row.resolve()
		if now.Before(job.UpdatedAt) {
			continue
		}
		work, err := archive.DecodeDiscoveryDetailJob(job)
		if err != nil {
			return nil, err
		}
		err = discoveryDetailEligible(ctx, work, now)
		if err != nil && !errors.Is(err, models.ErrDiscoveryConflict) {
			return nil, err
		}
		if err != nil {
			if _, err := (&ArchiveJobStore{}).Cancel(ctx, job.UUID, job.Revision, now); err != nil {
				return nil, err
			}
			ret.Cancelled++
		} else if job.State == "running" && job.LeaseUntil != nil && !now.Before(*job.LeaseUntil) {
			if err := recoverArchiveJob(ctx, job, now); err != nil {
				return nil, err
			}
			ret.Recovered++
		}
	}
	*complete = true
	return ret, nil
}
