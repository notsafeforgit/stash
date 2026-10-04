package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

// InspectionCollections returns permitted containers, not a promise of runnable
// work. Candidate inspection below bounds both target and listing traversal;
// it applies current review, history and due-time guards before admission.
func (s *DiscoveryDetailStore) InspectionCollections(ctx context.Context, q models.EnrichmentCollectionQuery, now time.Time) ([]models.EnrichmentCollectionCandidate, error) {
	limit, err := validateDiscoveryDetailCollections(q, now)
	if err != nil {
		return nil, err
	}
	permissions := []string{}
	args := []any{q.After}
	for _, scope := range q.Scopes {
		permissions = append(permissions, "(b.uuid=? AND d.root_uuid IS ?)")
		args = append(args, scope.CollectionUUID, scope.RootUUID)
	}
	for _, root := range q.Roots {
		permissions = append(permissions, "d.root_uuid=?")
		args = append(args, root)
	}
	// The collection index needs only the existence of a retained listing.
	// Do not search all of its targets to decide whether to visit a container.
	query := `SELECT b.uuid FROM source_collections b
 JOIN source_collection_revisions d ON d.collection_uuid=b.uuid AND d.revision=b.revision
 WHERE b.uuid>? AND d.state='active' AND (` + strings.Join(permissions, " OR ") + `)
 AND EXISTS(SELECT 1 FROM discovery_listings l WHERE l.collection_uuid=b.uuid)
 ORDER BY b.uuid LIMIT ?`
	args = append(args, limit)
	ret := []models.EnrichmentCollectionCandidate{}
	if err := dbWrapper.Select(ctx, &ret, query, args...); err != nil {
		return nil, err
	}
	return ret, nil
}

// automaticDiscoveryDetailCandidate uses compact references, never page or
// transcript bodies. Any previous attempt for this candidate requires explicit
// review/retry, regardless of a later comparison revision or changed profile.
// allowedJob is used only to recheck a just-admitted job before commit.
func automaticDiscoveryDetailCandidate(ctx context.Context, id, allowedJob string, now time.Time) (*models.DiscoveryDetailCandidate, error) {
	review, err := (&DiscoveryMatchStore{}).Review(ctx, id)
	if err != nil || review == nil {
		return nil, err
	}
	if review.Publication != nil || review.Detail != nil || !review.Coverage.Complete ||
		review.Candidate == nil || !review.Candidate.NeedsDetail ||
		len(review.Blockers) != 1 || review.Blockers[0] != "detail_required" || now.Before(review.Target.UpdatedAt) {
		return nil, nil
	}
	var previous bool
	if err := dbWrapper.Get(ctx, &previous, `SELECT EXISTS(SELECT 1 FROM discovery_detail_jobs
 WHERE target_uuid=? AND candidate_sequence=? AND job_uuid!=?)`, id, review.Candidate.Sequence, allowedJob); err != nil {
		return nil, err
	}
	if previous {
		return nil, nil
	}
	listing, err := (&DiscoveryJobStore{}).Listing(ctx, review.Target.ListingUUID)
	if err != nil {
		return nil, err
	}
	if listing == nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	if now.Before(listing.NotBefore) {
		return nil, nil
	}
	return &models.DiscoveryDetailCandidate{
		Cursor:     models.DiscoveryDetailCursor{ListingUUID: listing.UUID, SourceOrdinal: review.Target.SourceOrdinal},
		TargetUUID: id, TargetRevision: review.Target.Revision, CandidateSequence: review.Candidate.Sequence,
		Namespace: review.Candidate.Namespace, Value: review.Candidate.Value, URL: review.Candidate.URL,
	}, nil
}

// Candidates visits at most 32 targets and 32 listing definitions. The existing
// collection/listing index and unique listing/snapshot/ordinal index provide
// keyset traversal without sorting or scanning another collection's history.
func (s *DiscoveryDetailStore) Candidates(ctx context.Context, collection string, after *models.DiscoveryDetailCursor, limit int, now time.Time) (*models.DiscoveryDetailCandidates, error) {
	if !validSourceRunUUID(collection) || !validJobTime(now) ||
		(after != nil && (!validSourceRunUUID(after.ListingUUID) || after.SourceOrdinal < 0)) {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	remaining := min(limit, 32)
	ret := &models.DiscoveryDetailCandidates{Candidates: []models.DiscoveryDetailCandidate{}}
	listingAfter, ordinalAfter := "", int64(0)
	if after != nil {
		copied := *after
		ret.After = &copied
		listingAfter, ordinalAfter = after.ListingUUID, after.SourceOrdinal
	}
	comparison := ">="
	for inspected := 0; inspected < 32; inspected++ {
		var listing struct {
			UUID     string  `db:"uuid"`
			Snapshot *string `db:"snapshot_uuid"`
		}
		err := dbWrapper.Get(ctx, &listing, `SELECT d.uuid,l.snapshot_uuid FROM discovery_listings d
 LEFT JOIN discovery_listing_legacy l ON l.listing_uuid=d.uuid
 WHERE d.collection_uuid=? AND d.uuid`+comparison+`? ORDER BY d.uuid LIMIT 1`, collection, listingAfter)
		if errors.Is(err, sql.ErrNoRows) {
			return ret, nil
		}
		if err != nil {
			return nil, err
		}
		if listing.UUID != listingAfter {
			ordinalAfter = 0
		}
		ret.After = &models.DiscoveryDetailCursor{ListingUUID: listing.UUID, SourceOrdinal: ordinalAfter}
		var rows []struct {
			UUID    string `db:"uuid"`
			Ordinal int64  `db:"source_ordinal"`
		}
		if listing.Snapshot != nil {
			err = dbWrapper.Select(ctx, &rows, `SELECT uuid,source_ordinal FROM discovery_match_targets
 WHERE listing_uuid=? AND snapshot_uuid=? AND source_ordinal>? ORDER BY source_ordinal LIMIT ?`, listing.UUID, *listing.Snapshot, ordinalAfter, remaining)
			if err != nil {
				return nil, err
			}
		}
		for _, row := range rows {
			ret.After.SourceOrdinal = row.Ordinal
			candidate, err := automaticDiscoveryDetailCandidate(ctx, row.UUID, "", now)
			if err != nil {
				return nil, err
			}
			if candidate != nil {
				ret.Candidates = append(ret.Candidates, *candidate)
			}
		}
		remaining -= len(rows)
		if remaining == 0 {
			ret.HasMore = true
			return ret, nil
		}
		listingAfter, ordinalAfter, comparison = listing.UUID, 0, ">"
	}
	ret.HasMore = true
	return ret, nil
}
