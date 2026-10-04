package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/txn"
)

type DiscoveryMatchStore struct{}

const maxDiscoveryMatchTargets = 10000

type discoveryMatchSourceRow struct {
	Data     string `db:"data"`
	SHA256   string `db:"data_sha256"`
	PostUUID string `db:"post_uuid"`
}

func discoveryTargetUUID(listing string, ordinal int64) string {
	id, _ := archive.DiscoveryTargetIdentity(listing, ordinal)
	return id
}

func discoveryMatchSource(get enrichmentGet, listing *models.DiscoveryListing, ordinal int64) (*discoveryMatchSourceRow, map[string]any, error) {
	if listing.Legacy == nil || ordinal < 1 {
		return nil, nil, models.ErrDiscoveryInvalid
	}
	var row discoveryMatchSourceRow
	err := get(&row, `SELECT e.data,e.data_sha256,r.post_uuid FROM automation_discovery_records r
 JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 JOIN automation_discovery_imports i ON i.snapshot_uuid=r.snapshot_uuid
 WHERE r.snapshot_uuid=? AND r.ordinal=? AND r.account_ordinal=? AND r.account_uuid=? AND r.collection_uuid=?
 AND r.outcome='mapped' AND r.disposition='held' AND e.source_table='discovery_targets' AND i.state!='running'`,
		listing.Legacy.SnapshotUUID, ordinal, listing.Legacy.AccountOrdinal, listing.AccountUUID, listing.CollectionUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, models.ErrDiscoveryConflict
	}
	if err != nil {
		return nil, nil, err
	}
	if sourceDigest([]byte(row.Data)) != row.SHA256 {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	previous, err := discoveryRecoveryTarget(get, listing, ordinal)
	if err != nil {
		return nil, nil, err
	}
	if previous != nil && (previous.SourceSHA256 != row.SHA256 || previous.PostUUID != row.PostUUID || previous.SnapshotUUID != listing.Legacy.SnapshotUUID) {
		return nil, nil, models.ErrDiscoveryConflict
	}
	document, err := archive.DecodeJSONObject([]byte(row.Data), scrape.CatalogChunkLimit)
	if err != nil {
		return nil, nil, err
	}
	values, ok := document["values"].(map[string]any)
	if !ok {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	if _, err := scrape.MatchDiscoveryListing(values, json.RawMessage(`{}`)); err != nil {
		return nil, nil, err
	}
	// Historical candidate bodies need a separate verified conversion. They
	// cannot silently disappear when a resumed listing starts at a saved cursor.
	var staged bool
	if err := get(&staged, `SELECT EXISTS(SELECT 1 FROM automation_discovery_records
 WHERE snapshot_uuid=? AND target_ordinal=? AND disposition='candidate')`, listing.Legacy.SnapshotUUID, ordinal); err != nil {
		return nil, nil, err
	}
	if staged {
		return nil, nil, models.ErrDiscoveryConflict
	}
	return &row, values, nil
}

func (s *DiscoveryMatchStore) Target(ctx context.Context, id string) (*models.DiscoveryMatchTarget, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrDiscoveryInvalid
	}
	var row models.DiscoveryMatchTarget
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM discovery_match_targets WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.CreatedAt, row.UpdatedAt = row.CreatedAt.UTC(), row.UpdatedAt.UTC()
	return &row, nil
}

func checkDiscoveryMatchTarget(ctx context.Context, target *models.DiscoveryMatchTarget, now time.Time) error {
	if err := discoveryMatchPost(ctx, target.PostUUID, target.PostRevision); err != nil {
		return err
	}
	listing, err := (&DiscoveryJobStore{}).Listing(ctx, target.ListingUUID)
	if err != nil {
		return err
	}
	if listing == nil {
		return models.ErrDiscoveryConflict
	}
	return discoveryListingEligible(ctx, listing.DiscoveryListingInput, maxTime(now, listing.NotBefore))
}

func discoveryMatchPost(ctx context.Context, id string, revision int) error {
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, id)
	if err != nil {
		return err
	}
	if post == nil || post.State != "active" || post.Revision != revision {
		return models.ErrDiscoveryConflict
	}
	return nil
}

func (s *DiscoveryMatchStore) BindTarget(ctx context.Context, input models.DiscoveryTargetInput, now time.Time) (*models.DiscoveryMatchTarget, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(input.ListingUUID) || input.SourceOrdinal < 1 || !archive.ValidSHA256(input.ExpectedSourceSHA256) || input.ExpectedPostRevision < 1 || !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	id := discoveryTargetUUID(input.ListingUUID, input.SourceOrdinal)
	prior, err := s.Target(ctx, id)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.SourceSHA256 != input.ExpectedSourceSHA256 || prior.PostRevision != input.ExpectedPostRevision {
			return nil, models.ErrDiscoveryConflict
		}
		return prior, nil
	}
	listing, err := (&DiscoveryJobStore{}).Listing(ctx, input.ListingUUID)
	if err != nil {
		return nil, err
	}
	if listing == nil || now.Before(listing.CreatedAt) {
		return nil, models.ErrDiscoveryConflict
	}
	source, _, err := discoveryMatchSource(func(out any, q string, args ...any) error { return dbWrapper.Get(ctx, out, q, args...) }, listing, input.SourceOrdinal)
	if err != nil {
		return nil, err
	}
	if source.SHA256 != input.ExpectedSourceSHA256 {
		return nil, models.ErrDiscoveryConflict
	}
	target := &models.DiscoveryMatchTarget{UUID: id, ListingUUID: listing.UUID, SnapshotUUID: listing.Legacy.SnapshotUUID, SourceOrdinal: input.SourceOrdinal,
		SourceSHA256: source.SHA256, PostUUID: source.PostUUID, PostRevision: input.ExpectedPostRevision, Policy: scrape.DiscoveryListingMatchPolicy, Revision: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	if err := checkDiscoveryMatchTarget(ctx, target, now); err != nil {
		return nil, err
	}
	var count int
	if err := dbWrapper.Get(ctx, &count, "SELECT count(*) FROM (SELECT 1 FROM discovery_match_targets WHERE listing_uuid=? LIMIT ?)", listing.UUID, maxDiscoveryMatchTargets); err != nil {
		return nil, err
	}
	if count >= maxDiscoveryMatchTargets {
		return nil, models.ErrArchiveJobCapacity
	}
	complete := discoveryAtomic(ctx)
	_, err = dbWrapper.NamedExec(ctx, `INSERT INTO discovery_match_targets(uuid,listing_uuid,snapshot_uuid,source_ordinal,source_sha256,post_uuid,post_revision,policy,created_at,updated_at)
 VALUES(:uuid,:listing_uuid,:snapshot_uuid,:source_ordinal,:source_sha256,:post_uuid,:post_revision,:policy,:created_at,:updated_at)`, target)
	if err != nil {
		return nil, err
	}
	if listing.RecoveryOf != nil {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO discovery_recovery_targets VALUES(?,?)", id, discoveryTargetUUID(listing.RecoveryOf.ListingUUID, input.SourceOrdinal)); err != nil {
			return nil, err
		}
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
			return validateDiscoveryRecoveryTarget(func(out any, q string, args ...any) error { return dbWrapper.Get(ctx, out, q, args...) }, id)
		})
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error { return checkDiscoveryMatchTarget(ctx, target, now) })
	result, err := s.Target(ctx, id)
	*complete = err == nil
	return result, err
}

func (s *DiscoveryMatchStore) Receipt(ctx context.Context, target string, page int) (*models.DiscoveryMatchReceipt, error) {
	if !validSourceRunUUID(target) || page < 1 || page > archive.MaxDiscoveryPages {
		return nil, models.ErrDiscoveryInvalid
	}
	var receipt models.DiscoveryMatchReceipt
	err := dbWrapper.Get(ctx, &receipt, "SELECT * FROM discovery_match_pages WHERE target_uuid=? AND page_ordinal=?", target, page)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	receipt.CreatedAt = receipt.CreatedAt.UTC()
	return &receipt, nil
}

func discoveryMatchesDigest(matches []scrape.DiscoveryPageCandidate) (string, error) {
	body, err := json.Marshal(matches)
	if err != nil {
		return "", err
	}
	return sourceDigest(body), nil
}

// Advance is the transactional convenience operation. Background processing uses
// Prepare under a read transaction and commits only its small candidate set.
func (s *DiscoveryMatchStore) Advance(ctx context.Context, id string, after int, now time.Time) (*models.DiscoveryMatchReceipt, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	prepared, err := s.Prepare(ctx, id, after, now)
	if err != nil || prepared == nil {
		return nil, err
	}
	return prepared.Commit(ctx, now)
}

type preparedDiscoveryComparison struct {
	target  *models.DiscoveryMatchTarget
	receipt models.DiscoveryMatchReceipt
	matches []scrape.DiscoveryPageCandidate
	prior   bool
}

// Prepare compares one target with one original page without writes. A missing
// next page returns nil. The returned object exposes no mutable source data.
func (s *DiscoveryMatchStore) Prepare(ctx context.Context, id string, after int, now time.Time) (models.PreparedDiscoveryComparison, error) {
	if !validSourceRunUUID(id) || after < 0 || after >= archive.MaxDiscoveryPages || !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	prior, err := s.Receipt(ctx, id, after+1)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return &preparedDiscoveryComparison{receipt: *prior, prior: true}, nil
	}
	target, err := s.Target(ctx, id)
	if err != nil {
		return nil, err
	}
	if target == nil || target.LastPage != after || target.EnumerationComplete || now.Before(target.UpdatedAt) {
		return nil, models.ErrDiscoveryConflict
	}
	if err := checkDiscoveryMatchTarget(ctx, target, now); err != nil {
		return nil, err
	}
	listing, err := (&DiscoveryJobStore{}).Listing(ctx, target.ListingUUID)
	if err != nil {
		return nil, err
	}
	source, values, err := discoveryMatchSource(func(out any, q string, args ...any) error { return dbWrapper.Get(ctx, out, q, args...) }, listing, target.SourceOrdinal)
	if err != nil {
		return nil, err
	}
	if source.SHA256 != target.SourceSHA256 || source.PostUUID != target.PostUUID {
		return nil, models.ErrSourcePayloadCorrupt
	}
	page, err := (&DiscoveryJobStore{}).Page(ctx, listing.UUID, after+1)
	if err != nil || page == nil {
		return nil, err
	}
	if now.Before(page.CreatedAt) {
		return nil, models.ErrDiscoveryConflict
	}
	matches, err := scrape.MatchDiscoveryPage(values, page.Body)
	if err != nil {
		return nil, err
	}
	digest, err := discoveryMatchesDigest(matches)
	if err != nil {
		return nil, err
	}
	receipt := models.DiscoveryMatchReceipt{TargetUUID: id, ListingUUID: listing.UUID, PageOrdinal: after + 1, PageSHA256: page.Digest, MatchesSHA256: digest,
		CandidateCount: len(matches), Complete: page.Complete, CreatedAt: now.UTC()}
	return &preparedDiscoveryComparison{target: target, receipt: receipt, matches: matches}, nil
}

// Commit atomically retains references, cross-page grouping and progress. Exact
// receipts remain replayable after later native edits; fresh results must still
// match the reviewed source/post and original page checked by Prepare.
func (p *preparedDiscoveryComparison) Commit(ctx context.Context, now time.Time) (*models.DiscoveryMatchReceipt, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	s := &DiscoveryMatchStore{}
	receipt := p.receipt
	id := receipt.TargetUUID
	prior, err := s.Receipt(ctx, id, receipt.PageOrdinal)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.ListingUUID != receipt.ListingUUID || prior.PageSHA256 != receipt.PageSHA256 || prior.MatchesSHA256 != receipt.MatchesSHA256 {
			return nil, models.ErrDiscoveryConflict
		}
		return prior, nil
	}
	if p.prior || p.target == nil || now.Before(receipt.CreatedAt) {
		return nil, models.ErrDiscoveryConflict
	}
	target, err := s.Target(ctx, id)
	if err != nil {
		return nil, err
	}
	if target == nil || *target != *p.target {
		return nil, models.ErrDiscoveryConflict
	}
	if err := checkDiscoveryMatchTarget(ctx, target, now); err != nil {
		return nil, err
	}
	var samePage bool
	err = dbWrapper.Get(ctx, &samePage, `SELECT EXISTS(SELECT 1 FROM discovery_pages p
 JOIN automation_snapshot_records e ON e.snapshot_uuid=? AND e.ordinal=?
 WHERE p.listing_uuid=? AND p.ordinal=? AND p.digest=? AND p.complete=? AND e.data_sha256=?)`,
		target.SnapshotUUID, target.SourceOrdinal, target.ListingUUID, receipt.PageOrdinal, receipt.PageSHA256, receipt.Complete, target.SourceSHA256)
	if err != nil {
		return nil, err
	}
	if !samePage {
		return nil, models.ErrDiscoveryConflict
	}
	receipt.CreatedAt = now.UTC()
	complete := discoveryAtomic(ctx)
	_, err = dbWrapper.NamedExec(ctx, `INSERT INTO discovery_match_pages(target_uuid,listing_uuid,page_ordinal,page_sha256,matches_sha256,candidate_count,complete,created_at)
 VALUES(:target_uuid,:listing_uuid,:page_ordinal,:page_sha256,:matches_sha256,:candidate_count,:complete,:created_at)`, receipt)
	if err != nil {
		return nil, err
	}
	for _, match := range p.matches {
		if err := s.retain(ctx, receipt, match); err != nil {
			return nil, err
		}
	}
	var count int
	if err := dbWrapper.Get(ctx, &count, "SELECT count(*) FROM discovery_match_candidates WHERE target_uuid=?", id); err != nil {
		return nil, err
	}
	if count > archive.MaxDiscoveryRecords {
		return nil, models.ErrArchiveJobCapacity
	}
	_, err = dbWrapper.Exec(ctx, `UPDATE discovery_match_targets SET last_page=?,enumeration_complete=?,revision=revision+1,updated_at=? WHERE uuid=?`, receipt.PageOrdinal, receipt.Complete, receipt.CreatedAt, id)
	if err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error { return checkDiscoveryMatchTarget(ctx, target, now) })
	result, err := s.Receipt(ctx, id, receipt.PageOrdinal)
	*complete = err == nil
	return result, err
}

func (s *DiscoveryMatchStore) retain(ctx context.Context, receipt models.DiscoveryMatchReceipt, match scrape.DiscoveryPageCandidate) error {
	var previous struct {
		Best        int  `db:"best_page"`
		NeedsDetail bool `db:"needs_detail"`
	}
	err := dbWrapper.Get(ctx, &previous, `SELECT c.best_page,e.needs_detail FROM discovery_match_candidates c
 JOIN discovery_match_evidence e ON e.target_uuid=c.target_uuid AND e.page_ordinal=c.best_page AND e.namespace=c.namespace AND e.value=c.value
 WHERE c.target_uuid=? AND c.namespace=? AND c.value=?`, receipt.TargetUUID, match.Post.Namespace, match.Post.Value)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = dbWrapper.Exec(ctx, `INSERT INTO discovery_match_candidates(target_uuid,namespace,value,first_page,best_page,last_page,page_count) VALUES(?,?,?,?,?,?,1)`,
			receipt.TargetUUID, match.Post.Namespace, match.Post.Value, receipt.PageOrdinal, receipt.PageOrdinal, receipt.PageOrdinal)
	} else if err == nil {
		best := previous.Best
		if previous.NeedsDetail && !match.NeedsDetail {
			best = receipt.PageOrdinal
		}
		_, err = dbWrapper.Exec(ctx, `UPDATE discovery_match_candidates SET best_page=?,last_page=?,page_count=page_count+1 WHERE target_uuid=? AND namespace=? AND value=?`,
			best, receipt.PageOrdinal, receipt.TargetUUID, match.Post.Namespace, match.Post.Value)
	}
	if err != nil {
		return err
	}
	ordinals, err := json.Marshal(match.RecordOrdinals)
	if err != nil {
		return err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO discovery_match_evidence(target_uuid,page_ordinal,namespace,value,url,basis,needs_detail,record_ordinals) VALUES(?,?,?,?,?,?,?,?)`,
		receipt.TargetUUID, receipt.PageOrdinal, match.Post.Namespace, match.Post.Value, match.URL, match.Basis, match.NeedsDetail, string(ordinals))
	return err
}

const discoveryCandidateSelect = `SELECT c.*,e.url,e.basis,e.needs_detail FROM discovery_match_candidates c
 JOIN discovery_match_evidence e ON e.target_uuid=c.target_uuid AND e.page_ordinal=c.best_page AND e.namespace=c.namespace AND e.value=c.value`

func (s *DiscoveryMatchStore) Candidates(ctx context.Context, id string, after int64, limit int) ([]models.DiscoveryMatchCandidate, error) {
	if !validSourceRunUUID(id) || after < 0 {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	ret := []models.DiscoveryMatchCandidate{}
	err = dbWrapper.Select(ctx, &ret, discoveryCandidateSelect+" WHERE c.target_uuid=? AND c.id>? ORDER BY c.id LIMIT ?", id, after, limit)
	return ret, err
}

type discoveryMatchEvidenceRow struct {
	TargetUUID     string `db:"target_uuid"`
	PageOrdinal    int    `db:"page_ordinal"`
	Namespace      string `db:"namespace"`
	Value          string `db:"value"`
	URL            string `db:"url"`
	Basis          string `db:"basis"`
	NeedsDetail    bool   `db:"needs_detail"`
	RecordOrdinals string `db:"record_ordinals"`
}

func (row discoveryMatchEvidenceRow) resolve() (*models.DiscoveryMatchEvidence, error) {
	ret := &models.DiscoveryMatchEvidence{TargetUUID: row.TargetUUID, PageOrdinal: row.PageOrdinal, Namespace: row.Namespace, Value: row.Value, URL: row.URL, Basis: row.Basis, NeedsDetail: row.NeedsDetail}
	if err := json.Unmarshal([]byte(row.RecordOrdinals), &ret.RecordOrdinals); err != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	previous := -1
	for _, ordinal := range ret.RecordOrdinals {
		if ordinal <= previous || ordinal >= archive.MaxDiscoveryRecords {
			return nil, models.ErrSourcePayloadCorrupt
		}
		previous = ordinal
	}
	if previous < 0 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return ret, nil
}

func (s *DiscoveryMatchStore) Evidence(ctx context.Context, candidate int64, after, limit int) ([]models.DiscoveryMatchEvidence, error) {
	if candidate < 1 || after < 0 || after > archive.MaxDiscoveryPages {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	var rows []discoveryMatchEvidenceRow
	err = dbWrapper.Select(ctx, &rows, `SELECT e.* FROM discovery_match_candidates c JOIN discovery_match_evidence e
 ON e.target_uuid=c.target_uuid AND e.namespace=c.namespace AND e.value=c.value
 WHERE c.id=? AND e.page_ordinal>? ORDER BY e.page_ordinal LIMIT ?`, candidate, after, limit)
	if err != nil {
		return nil, err
	}
	ret := []models.DiscoveryMatchEvidence{}
	for _, row := range rows {
		value, err := row.resolve()
		if err != nil {
			return nil, err
		}
		ret = append(ret, *value)
	}
	return ret, nil
}
