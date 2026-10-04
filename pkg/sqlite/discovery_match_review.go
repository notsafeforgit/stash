package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/stashapp/stash/pkg/models"
)

// Review uses only indexed definitions, page receipts and candidate references.
// Callers use one read transaction so progress and current native choices belong
// to the same snapshot. No page bodies, jobs or identity mutations are needed.
func (s *DiscoveryMatchStore) Review(ctx context.Context, id string) (*models.DiscoveryMatchReview, error) {
	target, err := s.Target(ctx, id)
	if err != nil || target == nil {
		return nil, err
	}
	listing, err := (&DiscoveryJobStore{}).Listing(ctx, target.ListingUUID)
	if err != nil {
		return nil, err
	}
	if listing == nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	ret := &models.DiscoveryMatchReview{Target: *target, Blockers: []string{}, Coverage: models.DiscoveryMatchCoverage{
		HistoricalPages: listing.HistoricalPages, StartsAtSavedCursor: len(listing.InitialCursor) != 0,
	}}
	var head struct {
		Ordinal  int  `db:"ordinal"`
		Complete bool `db:"complete"`
	}
	err = dbWrapper.Get(ctx, &head, "SELECT ordinal,complete FROM discovery_pages WHERE listing_uuid=? ORDER BY ordinal DESC LIMIT 1", listing.UUID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if target.LastPage > head.Ordinal || (target.EnumerationComplete && (target.LastPage != head.Ordinal || !head.Complete)) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	ret.Coverage.RetainedPages, ret.Coverage.RetainedComplete = head.Ordinal, head.Complete
	if listing.HistoricalPages != 0 || len(listing.InitialCursor) != 0 {
		ret.Blockers = append(ret.Blockers, "history_not_retained")
	}
	if !head.Complete {
		ret.Blockers = append(ret.Blockers, "listing_incomplete")
	}
	if target.LastPage != head.Ordinal || (head.Complete && !target.EnumerationComplete) {
		ret.Blockers = append(ret.Blockers, "comparison_pending")
	}
	ret.Coverage.Complete = listing.HistoricalPages == 0 && len(listing.InitialCursor) == 0 && head.Complete && target.EnumerationComplete && target.LastPage == head.Ordinal

	// A future retry deadline concerns fetching. Inspecting retained evidence
	// still checks the original account, collection revision and root choices.
	if err := discoveryListingEligible(ctx, listing.DiscoveryListingInput, listing.NotBefore); err != nil {
		if !errors.Is(err, models.ErrDiscoveryConflict) {
			return nil, err
		}
		ret.Blockers = append(ret.Blockers, "source_changed")
	}
	ret.CurrentPost, err = (&SourceEvidenceStore{}).FindPost(ctx, target.PostUUID)
	if err != nil {
		return nil, err
	}
	if ret.CurrentPost == nil || ret.CurrentPost.State != "active" || ret.CurrentPost.Revision != target.PostRevision {
		ret.Blockers = append(ret.Blockers, "post_changed")
	}
	var identified bool
	if err := dbWrapper.Get(ctx, &identified, `SELECT EXISTS(SELECT 1 FROM source_post_identifiers WHERE post_uuid=? AND namespace NOT LIKE 'legacy:catalog:%')`, target.PostUUID); err != nil {
		return nil, err
	}
	if identified {
		ret.Blockers = append(ret.Blockers, "post_already_identified")
	}
	var counts struct {
		Candidates int `db:"candidates"`
		Details    int `db:"details"`
	}
	err = dbWrapper.Get(ctx, &counts, `SELECT count(*) AS candidates,coalesce(sum(e.needs_detail),0) AS details
 FROM discovery_match_candidates c JOIN discovery_match_evidence e
 ON e.target_uuid=c.target_uuid AND e.page_ordinal=c.best_page AND e.namespace=c.namespace AND e.value=c.value
 WHERE c.target_uuid=?`, id)
	if err != nil {
		return nil, err
	}
	ret.CandidateCount, ret.DetailCandidateCount = counts.Candidates, counts.Details
	if counts.Candidates == 0 && ret.Coverage.Complete {
		ret.Blockers = append(ret.Blockers, "no_candidate")
	}
	if counts.Candidates > 1 {
		ret.Blockers = append(ret.Blockers, "competing_candidates")
	}
	if counts.Details != 0 {
		ret.Blockers = append(ret.Blockers, "detail_required")
	}
	if counts.Candidates == 1 {
		candidates, err := s.Candidates(ctx, id, 0, 1)
		if err != nil {
			return nil, err
		}
		if len(candidates) != 1 {
			return nil, models.ErrSourcePayloadCorrupt
		}
		ret.Candidate = &candidates[0]
		ret.CandidatePost, err = (&SourceEvidenceStore{}).FindPostByIdentifier(ctx, models.SourcePostIdentifier{Namespace: ret.Candidate.Namespace, Value: ret.Candidate.Value})
		if err != nil {
			return nil, err
		}
		if ret.CandidatePost != nil && ret.CandidatePost.UUID != target.PostUUID {
			// Forgotten posts also retain their identifiers. Never revive or
			// consolidate a different native post as a review side effect.
			ret.Blockers = append(ret.Blockers, "identifier_in_use")
		}
	}
	return ret, nil
}
