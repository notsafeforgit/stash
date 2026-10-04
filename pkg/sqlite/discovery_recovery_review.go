package sqlite

import "github.com/stashapp/stash/pkg/models"

func discoveryRecoveryReview(get enrichmentGet, listing *models.DiscoveryListing, target *models.DiscoveryMatchTarget, candidate *models.DiscoveryMatchCandidate) (*models.DiscoveryMatchRecovery, error) {
	if listing.RecoveryOf == nil {
		return nil, nil
	}
	if err := validateDiscoveryRecoveryTarget(get, target.UUID); err != nil {
		return nil, err
	}
	previous, err := discoveryRecoveryTarget(get, listing, target.SourceOrdinal)
	if err != nil {
		return nil, err
	}
	ret := &models.DiscoveryMatchRecovery{ListingUUID: previous.ListingUUID, TargetUUID: previous.UUID}
	var pages int
	if err := get(&pages, "SELECT coalesce(max(ordinal),0) FROM discovery_pages WHERE listing_uuid=?", previous.ListingUUID); err != nil {
		return nil, err
	}
	ret.UncomparedPages = pages - previous.LastPage
	if ret.UncomparedPages < 0 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	namespace, value := "", ""
	if candidate != nil {
		namespace, value = candidate.Namespace, candidate.Value
	}
	var counts struct {
		Candidates int `db:"candidates"`
		Conflicts  int `db:"conflicts"`
	}
	if err := get(&counts, `SELECT count(*) AS candidates,coalesce(sum(namespace!=? OR value!=?),0) AS conflicts
 FROM discovery_match_candidates WHERE target_uuid=?`, namespace, value, previous.UUID); err != nil {
		return nil, err
	}
	ret.CandidateCount, ret.ConflictingCandidates = counts.Candidates, counts.Conflicts
	return ret, nil
}
