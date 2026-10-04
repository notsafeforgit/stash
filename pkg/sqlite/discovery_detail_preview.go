package sqlite

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

// PreviewDetail uses selected indexed reads in one read transaction. It does
// not accept producer evidence or change the current candidate/review outcome.
func (s *DiscoveryMatchStore) PreviewDetail(ctx context.Context, input models.DiscoveryDetailPreviewInput) (*models.DiscoveryDetailPreview, error) {
	if !validSourceRunUUID(input.TargetUUID) || input.ExpectedTargetRevision < 1 || input.CandidateSequence < 1 ||
		input.ExtractorVersion == "" || len(input.ExtractorVersion) > 128 || !utf8.ValidString(input.ExtractorVersion) ||
		strings.ContainsAny(input.ExtractorVersion, "\r\n\x00") || len(input.Body) == 0 || len(input.Body) > archive.MaxEnrichmentTranscriptBytes {
		return nil, models.ErrDiscoveryInvalid
	}
	review, err := s.Review(ctx, input.TargetUUID)
	if err != nil || review == nil {
		return nil, err
	}
	if review.Target.Revision != input.ExpectedTargetRevision || review.Publication != nil {
		return nil, models.ErrDiscoveryConflict
	}
	candidates, err := s.Candidates(ctx, input.TargetUUID, input.CandidateSequence-1, 1)
	if err != nil {
		return nil, err
	}
	if len(candidates) != 1 || candidates[0].Sequence != input.CandidateSequence || !candidates[0].NeedsDetail {
		return nil, models.ErrDiscoveryConflict
	}
	candidate := candidates[0]
	listing, err := (&DiscoveryJobStore{}).Listing(ctx, review.Target.ListingUUID)
	if err != nil {
		return nil, err
	}
	if listing == nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	source, values, err := discoveryMatchSource(func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) }, listing, review.Target.SourceOrdinal)
	if err != nil {
		return nil, err
	}
	if source.SHA256 != review.Target.SourceSHA256 || source.PostUUID != review.Target.PostUUID {
		return nil, models.ErrSourcePayloadCorrupt
	}
	page, err := (&DiscoveryJobStore{}).Page(ctx, listing.UUID, candidate.BestPage)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	evidence, err := scrape.MatchDiscoveryDetail(values, page.Body, models.SourcePostIdentifier{Namespace: candidate.Namespace, Value: candidate.Value}, input.ExtractorVersion, input.Body)
	if err != nil {
		return nil, err
	}
	if evidence.PageSHA256 != page.Digest || evidence.URL != candidate.URL {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.DiscoveryDetailPreview{PreviewOnly: true, TargetUUID: input.TargetUUID, TargetRevision: review.Target.Revision,
		CandidateSequence: candidate.Sequence, Evidence: *evidence, Blockers: append([]string{}, review.Blockers...)}, nil
}
