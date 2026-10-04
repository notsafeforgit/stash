package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/txn"
)

func (s *DiscoveryMatchStore) Publication(ctx context.Context, id string) (*models.DiscoveryMatchPublication, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrDiscoveryInvalid
	}
	var row models.DiscoveryMatchPublication
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM discovery_match_publications WHERE target_uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	row.CreatedAt = row.CreatedAt.UTC()
	return &row, err
}

func (s *DiscoveryMatchStore) PublishedRecords(ctx context.Context, id string, after, limit int) ([]models.DiscoveryPublishedRecord, error) {
	if !validSourceRunUUID(id) || after < -1 || after >= archive.MaxDiscoveryRecords {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	ret := []models.DiscoveryPublishedRecord{}
	err = dbWrapper.Select(ctx, &ret, "SELECT * FROM discovery_published_records WHERE target_uuid=? AND ordinal>? ORDER BY ordinal LIMIT ?", id, after, limit)
	return ret, err
}

type preparedDiscoveryPublication struct {
	input     models.DiscoveryPublicationInput
	target    models.DiscoveryMatchTarget
	listing   *models.DiscoveryListing
	candidate models.DiscoveryMatchCandidate
	page      *models.DiscoveryPage
	records   []archive.DiscoveryCaptureRecord
	row       models.DiscoveryMatchPublication
	witness   time.Time
	prior     *models.DiscoveryMatchPublication
	detail    *preparedDiscoveryPublicationDetail
}

// prepareDiscoveryPublication derives the immutable publication from the saved
// source graph. Startup uses it after native choices have subsequently changed;
// new publication separately requires the current review to be eligible.
func prepareDiscoveryPublication(get enrichmentGet, selectRows enrichmentSelect, input models.DiscoveryPublicationInput) (*preparedDiscoveryPublication, error) {
	ret := &preparedDiscoveryPublication{input: input}
	if err := get(&ret.target, "SELECT * FROM discovery_match_targets WHERE uuid=?", input.TargetUUID); err != nil {
		return nil, err
	}
	ret.target.CreatedAt, ret.target.UpdatedAt = ret.target.CreatedAt.UTC(), ret.target.UpdatedAt.UTC()
	if ret.target.Revision != input.ExpectedTargetRevision || !ret.target.EnumerationComplete {
		return nil, models.ErrDiscoveryConflict
	}
	var listing discoveryListingRow
	if err := get(&listing, "SELECT * FROM discovery_listings WHERE uuid=?", ret.target.ListingUUID); err != nil {
		return nil, err
	}
	var err error
	ret.listing, err = listing.resolve()
	if err != nil {
		return nil, err
	}
	if ret.listing.HistoricalPages != 0 || len(ret.listing.InitialCursor) != 0 {
		return nil, models.ErrDiscoveryConflict
	}
	var last struct {
		Ordinal  int  `db:"ordinal"`
		Complete bool `db:"complete"`
	}
	if err := get(&last, "SELECT ordinal,complete FROM discovery_pages WHERE listing_uuid=? ORDER BY ordinal DESC LIMIT 1", ret.listing.UUID); err != nil {
		return nil, err
	}
	if !last.Complete || last.Ordinal != ret.target.LastPage {
		return nil, models.ErrDiscoveryConflict
	}
	var candidates []models.DiscoveryMatchCandidate
	if err := selectRows(&candidates, discoveryCandidateSelect+" WHERE c.target_uuid=? ORDER BY c.id LIMIT 2", ret.target.UUID); err != nil {
		return nil, err
	}
	if len(candidates) != 1 || candidates[0].NeedsDetail != (input.DetailJobUUID != "") {
		return nil, models.ErrDiscoveryConflict
	}
	ret.candidate = candidates[0]
	recovery, err := discoveryRecoveryReview(get, ret.listing, &ret.target, &ret.candidate)
	if err != nil {
		return nil, err
	}
	if recovery != nil && (recovery.UncomparedPages != 0 || recovery.ConflictingCandidates != 0) {
		return nil, models.ErrDiscoveryConflict
	}
	pageOrdinal := ret.candidate.BestPage
	if input.DetailJobUUID != "" {
		ret.detail, err = prepareDiscoveryPublicationDetail(get, selectRows, input.DetailJobUUID, ret.target, ret.listing, ret.candidate)
		if err != nil {
			return nil, err
		}
		pageOrdinal = ret.detail.work.PageOrdinal
	}
	var page discoveryPageRow
	if err := get(&page, "SELECT * FROM discovery_pages WHERE listing_uuid=? AND ordinal=?", ret.listing.UUID, pageOrdinal); err != nil {
		return nil, err
	}
	ret.page, err = page.resolve()
	if err != nil {
		return nil, err
	}
	source, values, err := discoveryMatchSource(get, ret.listing, ret.target.SourceOrdinal)
	if err != nil {
		return nil, err
	}
	if source.SHA256 != ret.target.SourceSHA256 || source.PostUUID != ret.target.PostUUID {
		return nil, models.ErrSourcePayloadCorrupt
	}
	matches, err := scrape.MatchDiscoveryPage(values, ret.page.Body)
	if err != nil {
		return nil, err
	}
	if len(matches) != 1 || matches[0].NeedsDetail != (ret.detail != nil) || matches[0].Post.Namespace != ret.candidate.Namespace || matches[0].Post.Value != ret.candidate.Value ||
		(ret.detail == nil && matches[0].Basis != ret.candidate.Basis) || matches[0].URL != ret.candidate.URL {
		return nil, models.ErrSourcePayloadCorrupt
	}
	digest, err := discoveryMatchesDigest(matches)
	if err != nil {
		return nil, err
	}
	var receipt models.DiscoveryMatchReceipt
	if err := get(&receipt, "SELECT * FROM discovery_match_pages WHERE target_uuid=? AND page_ordinal=?", ret.target.UUID, ret.page.Ordinal); err != nil {
		return nil, err
	}
	if receipt.PageSHA256 != ret.page.Digest || receipt.MatchesSHA256 != digest || receipt.CandidateCount != 1 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	parsed, err := archive.ParseDiscoveryPage(ret.page.Body)
	if err != nil {
		return nil, err
	}
	if parsed.URL != ret.listing.ProfileURL || parsed.ExtractorVersion != ret.listing.ExtractorVersion {
		return nil, models.ErrSourcePayloadCorrupt
	}
	witness := -1
	if ret.detail != nil {
		ret.records, ret.witness = ret.detail.records, ret.detail.witness
		witness = *ret.detail.result.Evidence.WitnessOrdinal
	} else {
		ret.records, err = archive.PrepareDiscoveryCaptures(*ret.page, ret.target.PostUUID, matches[0].Post)
		if err != nil {
			return nil, err
		}
		for _, record := range ret.records {
			raw, err := parsed.Metadata(record.Ordinal)
			if err != nil {
				return nil, err
			}
			match, err := scrape.MatchDiscoveryListing(values, raw)
			if err != nil {
				return nil, err
			}
			if match != nil && !match.NeedsDetail && match.Post == matches[0].Post && match.Basis == ret.candidate.Basis {
				witness, ret.witness = record.Ordinal, record.Input.CapturedAt
				break
			}
		}
	}
	if witness < 0 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	unique := make(map[string]bool)
	for _, record := range ret.records {
		unique[record.Input.UUID] = true
	}
	namespace := uuid.MustParse(ret.target.UUID)
	ret.row = models.DiscoveryMatchPublication{TargetUUID: ret.target.UUID, TargetRevision: ret.target.Revision, ListingUUID: ret.listing.UUID,
		PageOrdinal: ret.page.Ordinal, PageSHA256: ret.page.Digest, PostUUID: ret.target.PostUUID, PostRevision: ret.target.PostRevision,
		Namespace: ret.candidate.Namespace, Value: ret.candidate.Value, Policy: archive.DiscoveryPublicationPolicy, Basis: ret.candidate.Basis,
		WitnessOrdinal: witness, EvidenceUUID: uuid.NewSHA1(namespace, []byte("discovery-publication-identity-v1")).String(),
		URLEvidenceUUID: uuid.NewSHA1(namespace, []byte("discovery-publication-url-v1")).String(), RecordCount: len(ret.records), CaptureCount: len(unique)}
	if ret.detail != nil {
		ret.row.Policy, ret.row.Basis = archive.DiscoveryDetailPublicationPolicy, ret.detail.result.Evidence.Basis
		ret.row.DetailJobUUID = &ret.detail.result.JobUUID
	}
	return ret, nil
}

func (s *DiscoveryMatchStore) PreparePublication(ctx context.Context, input models.DiscoveryPublicationInput) (models.PreparedDiscoveryPublication, error) {
	if !validSourceRunUUID(input.TargetUUID) || input.ExpectedTargetRevision < 2 || input.ExpectedTargetRevision > archive.MaxDiscoveryPages+1 ||
		(input.DetailJobUUID != "" && !validSourceRunUUID(input.DetailJobUUID)) {
		return nil, models.ErrDiscoveryInvalid
	}
	prior, err := s.Publication(ctx, input.TargetUUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.TargetRevision != input.ExpectedTargetRevision || publicationDetailUUID(prior) != input.DetailJobUUID {
			return nil, models.ErrDiscoveryConflict
		}
		return &preparedDiscoveryPublication{input: input, prior: prior}, nil
	}
	review, err := s.Review(ctx, input.TargetUUID)
	if err != nil {
		return nil, err
	}
	if review == nil || len(review.Blockers) != 0 || review.Target.Revision != input.ExpectedTargetRevision || !discoveryPublicationDetailMatches(review, input.DetailJobUUID) {
		return nil, models.ErrDiscoveryConflict
	}
	return prepareDiscoveryPublication(func(out any, q string, args ...any) error { return dbWrapper.Get(ctx, out, q, args...) },
		func(out any, q string, args ...any) error { return dbWrapper.Select(ctx, out, q, args...) }, input)
}

func discoveryPublicationLinks(p *preparedDiscoveryPublication) (models.SourcePostIdentifierInput, models.SourcePostURLInput, error) {
	proof := struct {
		Target         string `json:"target_uuid"`
		Listing        string `json:"listing_uuid"`
		Page           int    `json:"page_ordinal"`
		Record         int    `json:"record_ordinal"`
		Policy         string `json:"policy"`
		DetailJob      string `json:"detail_job_uuid,omitempty"`
		DetailRevision int    `json:"detail_checkpoint_revision,omitempty"`
		DetailDigest   string `json:"detail_checkpoint_sha256,omitempty"`
	}{Target: p.target.UUID, Listing: p.listing.UUID, Page: p.page.Ordinal, Record: p.row.WitnessOrdinal, Policy: p.row.Policy}
	if p.detail != nil {
		proof.DetailJob, proof.DetailRevision, proof.DetailDigest = p.detail.result.JobUUID, p.detail.result.CheckpointRevision, p.detail.result.Evidence.TranscriptSHA256
	}
	details, err := json.Marshal(proof)
	if err != nil {
		return models.SourcePostIdentifierInput{}, models.SourcePostURLInput{}, err
	}
	identity := models.SourcePostIdentifierInput{
		SourcePostEvidence: models.SourcePostEvidence{UUID: p.row.EvidenceUUID, PostUUID: p.target.PostUUID, Origin: "capture", Basis: p.row.Basis, ObservedAt: p.witness, Details: details},
		Identifier:         models.SourcePostIdentifier{Namespace: p.row.Namespace, Value: p.row.Value}, ExpectedPostRevision: p.row.PostRevision,
	}
	url := models.SourcePostURLInput{SourcePostEvidence: models.SourcePostEvidence{UUID: p.row.URLEvidenceUUID, PostUUID: p.target.PostUUID, Origin: "capture",
		Basis: "canonical-url-for-verified-post", ObservedAt: p.witness, Details: details}, URL: p.candidate.URL}
	return identity, url, nil
}

func (p *preparedDiscoveryPublication) Publish(ctx context.Context, writer models.DiscoveryCaptureWriter, now time.Time) (*models.DiscoveryMatchPublication, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	s := &DiscoveryMatchStore{}
	prior, err := s.Publication(ctx, p.input.TargetUUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.TargetRevision != p.input.ExpectedTargetRevision || publicationDetailUUID(prior) != p.input.DetailJobUUID {
			return nil, models.ErrDiscoveryConflict
		}
		return prior, nil
	}
	if p.prior != nil || writer == nil || !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	review, err := s.Review(ctx, p.input.TargetUUID)
	if err != nil {
		return nil, err
	}
	if review == nil || len(review.Blockers) != 0 || !reflect.DeepEqual(review.Target, p.target) || review.Candidate == nil || !reflect.DeepEqual(*review.Candidate, p.candidate) ||
		!discoveryPublicationDetailMatches(review, p.input.DetailJobUUID) || now.Before(p.target.UpdatedAt) || now.Before(p.page.CreatedAt) ||
		(p.detail != nil && now.Before(p.detail.result.CreatedAt)) {
		return nil, models.ErrDiscoveryConflict
	}
	for _, record := range p.records {
		if now.Before(record.Input.CapturedAt) {
			return nil, models.ErrDiscoveryConflict
		}
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, p.listing.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if collection == nil || (collection.Namespace != "" && collection.Namespace != p.row.Namespace) {
		return nil, models.ErrDiscoveryConflict
	}
	complete := discoveryAtomic(ctx)
	identity, url, err := discoveryPublicationLinks(p)
	if err != nil {
		return nil, err
	}
	if _, err := (&SourcePostLinksStore{}).ObserveIdentifier(ctx, identity); err != nil {
		return nil, err
	}
	if _, err := (&SourcePostLinksStore{}).ObserveURL(ctx, url); err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, record := range p.records {
		if seen[record.Input.UUID] {
			continue
		}
		// Keep the prepared proof private even if a domain writer changes its
		// argument. Exact capture/provenance verification runs before commit.
		body, err := json.Marshal(record.Input)
		if err != nil {
			return nil, err
		}
		var input models.SourceCaptureInput
		if err := json.Unmarshal(body, &input); err != nil {
			return nil, err
		}
		copyCollection := *collection
		if err := writer(ctx, input, &copyCollection); err != nil {
			return nil, err
		}
		seen[record.Input.UUID] = true
	}
	row := p.row
	row.CreatedAt = now.UTC()
	_, err = dbWrapper.NamedExec(ctx, `INSERT INTO discovery_match_publications
 (target_uuid,target_revision,listing_uuid,page_ordinal,page_sha256,post_uuid,post_revision,namespace,value,policy,basis,witness_ordinal,evidence_uuid,url_evidence_uuid,record_count,capture_count,created_at,detail_job_uuid)
 VALUES(:target_uuid,:target_revision,:listing_uuid,:page_ordinal,:page_sha256,:post_uuid,:post_revision,:namespace,:value,:policy,:basis,:witness_ordinal,:evidence_uuid,:url_evidence_uuid,:record_count,:capture_count,:created_at,:detail_job_uuid)`, row)
	if err != nil {
		return nil, err
	}
	for _, record := range p.records {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO discovery_published_records VALUES(?,?,?)", row.TargetUUID, record.Ordinal, record.Input.UUID); err != nil {
			return nil, err
		}
	}
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, p.target.PostUUID)
	if err != nil {
		return nil, err
	}
	if post == nil || post.State != "active" {
		return nil, models.ErrDiscoveryConflict
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		if err := discoveryMatchPost(ctx, post.UUID, post.Revision); err != nil {
			return err
		}
		if err := discoveryListingEligible(ctx, p.listing.DiscoveryListingInput, p.listing.NotBefore); err != nil {
			return err
		}
		return verifyDiscoveryPublication(func(out any, q string, args ...any) error { return dbWrapper.Get(ctx, out, q, args...) },
			func(out any, q string, args ...any) error { return dbWrapper.Select(ctx, out, q, args...) }, row)
	})
	*complete = true
	return &row, nil
}
