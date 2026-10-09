package sqlite

import (
	"context"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// Collections discovers current work within explicit producer grants. The
// active job set is bounded independently of history; unadmitted targets use
// the same eligibility predicates as the selected-collection endpoint.
func (s *EnrichmentJobStore) Collections(ctx context.Context, q models.EnrichmentCollectionQuery, now time.Time) ([]models.EnrichmentCollectionCandidate, error) {
	if len(q.Scopes)+len(q.Roots) < 1 || len(q.Scopes)+len(q.Roots) > 128 ||
		!archive.ValidSHA256(q.PolicySHA256) || q.ExtractorVersion == "" || len(q.ExtractorVersion) > 128 || strings.ContainsAny(q.ExtractorVersion, "\r\n\x00") ||
		(q.After != "" && !validSourceRunUUID(q.After)) || !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	limit, err := sourcePageLimit(q.Limit)
	if err != nil {
		return nil, models.ErrEnrichmentInvalid
	}
	permissions := []string{}
	args := []any{q.After}
	for _, scope := range q.Scopes {
		if !validSourceRunUUID(scope.CollectionUUID) || (scope.RootUUID != nil && !validSourceRunUUID(*scope.RootUUID)) {
			return nil, models.ErrEnrichmentInvalid
		}
		permissions = append(permissions, "(b.uuid=? AND d.root_uuid IS ?)")
		args = append(args, scope.CollectionUUID, scope.RootUUID)
	}
	for _, root := range q.Roots {
		if !validSourceRunUUID(root) {
			return nil, models.ErrEnrichmentInvalid
		}
		permissions = append(permissions, "d.root_uuid=?")
		args = append(args, root)
	}
	rows, err := activeEnrichmentJobs(ctx)
	if err != nil {
		return nil, err
	}
	jobs := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		job := row.resolve()
		if job.State != "queued" || now.Before(job.AvailableAt) {
			continue
		}
		work, err := archive.DecodeEnrichmentJob(job)
		if err != nil {
			return nil, err
		}
		executionPolicy, _, err := metadataWorkerPolicy(ctx, job.Kind, work.PolicySHA256)
		if err != nil {
			return nil, err
		}
		if seen[work.CollectionUUID] || executionPolicy != q.PolicySHA256 || work.ExtractorVersion != q.ExtractorVersion {
			continue
		}
		if _, err := enrichmentJobEligible(ctx, work, now); err != nil {
			if staleEnrichmentSource(err) {
				continue
			}
			return nil, err
		}
		seen[work.CollectionUUID] = true
		jobs = append(jobs, "?")
		args = append(args, work.CollectionUUID)
	}
	queued := "0"
	if len(jobs) != 0 {
		queued = "b.uuid IN (" + strings.Join(jobs, ",") + ")"
	}
	query := `SELECT b.uuid FROM source_collections b JOIN source_collection_revisions d ON d.collection_uuid=b.uuid AND d.revision=b.revision
 WHERE b.uuid>? AND d.state='active' AND (` + strings.Join(permissions, " OR ") + ") AND (" + queued + ` OR EXISTS(
 SELECT 1 FROM enrichment_targets t` + enrichmentReadySources + " WHERE t.collection_uuid=b.uuid AND " + enrichmentReadyConditions + ")) ORDER BY b.uuid LIMIT ?"
	args = append(args, now.UTC(), limit)
	ret := []models.EnrichmentCollectionCandidate{}
	if err := dbWrapper.Select(ctx, &ret, query, args...); err != nil {
		return nil, err
	}
	return ret, nil
}
