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

type discoveryDetailResultRow struct {
	models.DiscoveryDetailResult
	Body string `db:"evidence"`
}

func (row discoveryDetailResultRow) resolve() (*models.DiscoveryDetailResult, error) {
	if err := json.Unmarshal([]byte(row.Body), &row.Evidence); err != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	row.CreatedAt = row.CreatedAt.UTC()
	return &row.DiscoveryDetailResult, nil
}
func (s *DiscoveryDetailStore) Result(ctx context.Context, id string) (*models.DiscoveryDetailResult, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrDiscoveryInvalid
	}
	var row discoveryDetailResultRow
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM discovery_detail_results WHERE job_uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.resolve()
}

func compareDiscoveryDetail(get enrichmentGet, work *models.DiscoveryDetailJobArguments, body json.RawMessage) (*models.DiscoveryDetailEvidence, error) {
	var row discoveryListingRow
	if err := get(&row, "SELECT * FROM discovery_listings WHERE uuid=?", work.ListingUUID); err != nil {
		return nil, err
	}
	listing, err := resolveDiscoveryListing(get, row)
	if err != nil {
		return nil, err
	}
	var target models.DiscoveryMatchTarget
	if err := get(&target, "SELECT * FROM discovery_match_targets WHERE uuid=?", work.TargetUUID); err != nil {
		return nil, err
	}
	source, values, err := discoveryMatchSource(get, listing, target.SourceOrdinal)
	if err != nil {
		return nil, err
	}
	if source.SHA256 != work.SourceSHA256 || source.PostUUID != work.PostUUID {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var page discoveryPageRow
	if err := get(&page, "SELECT * FROM discovery_pages WHERE listing_uuid=? AND ordinal=?", work.ListingUUID, work.PageOrdinal); err != nil {
		return nil, err
	}
	evidence, err := scrape.MatchDiscoveryDetail(values, []byte(page.Body), work.PostIdentifier(), work.ExtractorVersion, body)
	if err != nil {
		return nil, err
	}
	if evidence.PageSHA256 != work.PageSHA256 || evidence.PageSHA256 != page.Digest || evidence.URL != work.URL {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return evidence, nil
}

func (s *DiscoveryDetailStore) Complete(ctx context.Context, lease models.EnrichmentJobLease, revision int, digest string, now time.Time) (*models.DiscoveryDetailResult, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if revision < 1 || !archive.ValidSHA256(digest) || !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	if err := s.ownedAttempt(ctx, lease); err != nil {
		return nil, err
	}
	prior, err := s.Result(ctx, lease.JobUUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.CheckpointRevision != revision || prior.Evidence.TranscriptSHA256 != digest || prior.Fence != lease.Fence {
			return nil, models.ErrDiscoveryConflict
		}
		return prior, nil
	}
	job, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	work, err := archive.DecodeDiscoveryDetailJob(job)
	if err != nil {
		return nil, err
	}
	checkpoint, err := s.CheckpointHead(ctx, lease.JobUUID)
	if err != nil {
		return nil, err
	}
	if checkpoint == nil || checkpoint.Revision != revision || checkpoint.Digest != digest || checkpoint.PendingCount != 0 || now.Before(checkpoint.CreatedAt) {
		return nil, models.ErrDiscoveryConflict
	}
	evidence, err := compareDiscoveryDetail(func(out any, q string, args ...any) error { return dbWrapper.Get(ctx, out, q, args...) }, work, checkpoint.Body)
	if err != nil {
		return nil, err
	}
	if evidence.Status == "pending" || evidence.TranscriptSHA256 != digest {
		return nil, models.ErrDiscoveryConflict
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	complete := discoveryAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, "INSERT INTO discovery_detail_results VALUES(?,?,?,?,?)", lease.JobUUID, revision, lease.Fence, string(encoded), now.UTC())
	if err != nil {
		return nil, err
	}
	result, err := json.Marshal(map[string]any{"checkpoint_revision": revision, "checkpoint_sha256": digest})
	if err != nil {
		return nil, err
	}
	if _, err := (&ArchiveJobStore{}).Finish(ctx, lease.ArchiveJobLease, now, models.ArchiveJobOutcome{State: "succeeded", Result: result}); err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error { return discoveryDetailEligible(ctx, work, now) })
	ret, err := s.Result(ctx, lease.JobUUID)
	*complete = err == nil && ret != nil
	return ret, err
}
