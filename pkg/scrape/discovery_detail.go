package scrape

import (
	"encoding/json"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const DiscoveryDetailMatchPolicy = "retained-discovery-detail-v1"

// Detail evidence refers to the original listing and fetched transcript. A
// corroborated result is not identity acceptance: complete listing coverage,
// competing candidates, native choices and owned delivery remain separate.
type DiscoveryDetailMatch = models.DiscoveryDetailEvidence

// MatchDiscoveryDetail requires an actual weak candidate from the retained
// listing. The inferred fetch URL never becomes original target evidence.
// A negative or partial response cannot discard another candidate.
func MatchDiscoveryDetail(values map[string]any, pageBody json.RawMessage, selected models.SourcePostIdentifier,
	extractor string, detailBody json.RawMessage) (*DiscoveryDetailMatch, error) {
	page, err := archive.ParseDiscoveryPage(pageBody)
	if err != nil {
		return nil, err
	}
	candidates, err := MatchDiscoveryPage(values, page.Body())
	if err != nil {
		return nil, err
	}
	var candidate *DiscoveryPageCandidate
	for i := range candidates {
		if candidates[i].Post == selected {
			candidate = &candidates[i]
			break
		}
	}
	if candidate == nil || !candidate.NeedsDetail {
		return nil, models.ErrDiscoveryConflict
	}
	transcript, err := archive.ParseDiscoveryDetail(detailBody, selected, candidate.URL, extractor)
	if err != nil {
		return nil, err
	}
	evidence, err := prepareListingEvidence(values)
	if err != nil {
		return nil, err
	}
	ret := &DiscoveryDetailMatch{Policy: DiscoveryDetailMatchPolicy, Post: selected, URL: candidate.URL,
		PageSHA256: CatalogSnapshotSHA(page.Body()), TranscriptSHA256: CatalogSnapshotSHA(transcript.Body()),
		Status: "uncorroborated", RecordOrdinals: []int{}, PendingCount: len(transcript.Pending), UnresolvedCount: len(transcript.Unresolved)}
	for ordinal := range transcript.Records {
		raw, err := transcript.Metadata(ordinal)
		if err != nil {
			return nil, err
		}
		// Reject a contradictory publisher anywhere in this one-post response,
		// even if another attachment contains corroborating text.
		account, err := archive.ExtractCapturedAccount(raw)
		if err != nil {
			return nil, models.ErrDiscoveryInvalid
		}
		if account != nil && evidence.account.Kind == "id" {
			for _, id := range account.Identifiers {
				if id.Reference.Namespace == evidence.account.Namespace && id.Reference.Kind == "id" && id.Reference != evidence.account {
					return nil, models.ErrDiscoveryConflict
				}
			}
		}
		ret.RecordOrdinals = append(ret.RecordOrdinals, ordinal)
		match, err := evidence.match(raw)
		if err != nil {
			return nil, models.ErrDiscoveryInvalid
		}
		if match != nil && !match.NeedsDetail && ret.WitnessOrdinal == nil {
			ret.WitnessOrdinal, ret.Basis = &ordinal, match.Basis
		}
	}
	if ret.PendingCount != 0 {
		ret.Status = "pending"
		ret.WitnessOrdinal, ret.Basis = nil, ""
	} else if ret.WitnessOrdinal != nil {
		ret.Status = "corroborated"
	}
	return ret, nil
}
