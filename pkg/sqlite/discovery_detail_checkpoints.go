package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

func (s *DiscoveryDetailStore) CheckpointHead(ctx context.Context, id string) (*models.EnrichmentCheckpoint, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrDiscoveryInvalid
	}
	var row struct {
		models.EnrichmentCheckpointReceipt
		Body string `db:"body"`
	}
	err := dbWrapper.Get(ctx, &row, `SELECT r.*,h.body FROM discovery_detail_checkpoints h
 JOIN discovery_detail_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision WHERE h.job_uuid=?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(row.Body) > archive.MaxEnrichmentTranscriptBytes || enrichmentDigest([]byte(row.Body)) != row.Digest {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.EnrichmentCheckpoint{EnrichmentCheckpointReceipt: row.EnrichmentCheckpointReceipt, Body: json.RawMessage(row.Body)}, nil
}

func (s *DiscoveryDetailStore) CheckpointReceipts(ctx context.Context, id string, after, limit int) ([]models.EnrichmentCheckpointReceipt, error) {
	if !validSourceRunUUID(id) || after < 0 {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	ret := []models.EnrichmentCheckpointReceipt{}
	err = dbWrapper.Select(ctx, &ret, "SELECT * FROM discovery_detail_checkpoint_receipts WHERE job_uuid=? AND revision>? ORDER BY revision LIMIT ?", id, after, limit)
	return ret, err
}

func (s *DiscoveryDetailStore) CheckpointRecords(ctx context.Context, id string, after, limit int) ([]models.EnrichmentCheckpointRecord, error) {
	if !validSourceRunUUID(id) || after < -1 {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	ret := []models.EnrichmentCheckpointRecord{}
	err = dbWrapper.Select(ctx, &ret, `SELECT r.*,c.fence,a.producer_uuid FROM discovery_detail_checkpoint_records r
 JOIN discovery_detail_checkpoint_receipts c ON c.job_uuid=r.job_uuid AND c.revision=r.checkpoint_revision
 JOIN discovery_detail_attempts a ON a.job_uuid=c.job_uuid AND a.fence=c.fence
 WHERE r.job_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?`, id, after, limit)
	return ret, err
}

func (s *DiscoveryDetailStore) Checkpoint(ctx context.Context, lease models.EnrichmentJobLease, expected int, raw json.RawMessage, now time.Time) (*models.EnrichmentCheckpointReceipt, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if expected < 0 || !validJobTime(now) {
		return nil, models.ErrDiscoveryInvalid
	}
	if err := s.ownedAttempt(ctx, lease); err != nil {
		return nil, err
	}
	parsed, err := archive.ParseEnrichmentTranscript(raw)
	if err != nil {
		return nil, models.ErrDiscoveryInvalid
	}
	body := parsed.Body()
	digest := enrichmentDigest(body)
	var receipt models.EnrichmentCheckpointReceipt
	err = dbWrapper.Get(ctx, &receipt, "SELECT * FROM discovery_detail_checkpoint_receipts WHERE job_uuid=? AND digest=?", lease.JobUUID, digest)
	if err == nil && receipt.Fence == lease.Fence {
		// The original owner can confirm a lost acknowledgement after expiry,
		// cancellation or a target edit. This creates no new work or ownership.
		return &receipt, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	current, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	if receipt.JobUUID != "" {
		return &receipt, nil // another current attempt resumes unchanged evidence
	}
	work, err := archive.DecodeDiscoveryDetailJob(current)
	if err != nil {
		return nil, err
	}
	parsed, err = archive.ParseDiscoveryDetail(body, work.PostIdentifier(), work.URL, work.ExtractorVersion)
	if err != nil {
		return nil, err
	}
	for _, record := range parsed.Records {
		observed, err := time.Parse(time.RFC3339Nano, record.ObservedAt)
		if err != nil || observed.After(now) {
			return nil, models.ErrDiscoveryInvalid
		}
	}
	prior, err := s.CheckpointHead(ctx, lease.JobUUID)
	if err != nil {
		return nil, err
	}
	revision, start, previousBytes := 0, 0, 0
	if prior != nil {
		revision, start, previousBytes = prior.Revision, prior.RecordCount, len(prior.Body)
		before, err := archive.ParseEnrichmentTranscript(prior.Body)
		if err != nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
		if !parsed.Extends(before) {
			return nil, models.ErrDiscoveryConflict
		}
		if now.Before(prior.CreatedAt) {
			return nil, models.ErrDiscoveryInvalid
		}
	}
	if expected != revision {
		return nil, models.ErrDiscoveryConflict
	}
	if revision >= archive.MaxEnrichmentCheckpointRevisions {
		return nil, models.ErrArchiveJobCapacity
	}
	var used int64
	if err := dbWrapper.Get(ctx, &used, "SELECT byte_size FROM discovery_detail_checkpoint_usage WHERE singleton=1"); err != nil {
		return nil, err
	}
	if int64(len(body)-previousBytes) > archive.MaxEnrichmentStoredBytes-used {
		return nil, models.ErrArchiveJobCapacity
	}
	complete := discoveryAtomic(ctx)
	receipt = models.EnrichmentCheckpointReceipt{JobUUID: lease.JobUUID, Revision: revision + 1, Digest: digest, Fence: lease.Fence,
		RecordCount: len(parsed.Records), PendingCount: len(parsed.Pending), UnresolvedCount: len(parsed.Unresolved), CreatedAt: now.UTC()}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO discovery_detail_checkpoint_receipts(job_uuid,revision,digest,fence,record_count,pending_count,unresolved_count,created_at)
 VALUES(?,?,?,?,?,?,?,?)`, receipt.JobUUID, receipt.Revision, receipt.Digest, receipt.Fence, receipt.RecordCount, receipt.PendingCount, receipt.UnresolvedCount, receipt.CreatedAt)
	if err != nil {
		return nil, err
	}
	for ordinal := start; ordinal < receipt.RecordCount; ordinal++ {
		digest, err := parsed.RecordDigest(ordinal)
		if err != nil {
			return nil, err
		}
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO discovery_detail_checkpoint_records VALUES(?,?,?,?)", lease.JobUUID, ordinal, receipt.Revision, digest); err != nil {
			return nil, err
		}
	}
	if prior == nil {
		_, err = dbWrapper.Exec(ctx, "INSERT INTO discovery_detail_checkpoints VALUES(?,?,?,?)", lease.JobUUID, receipt.Revision, string(body), len(body))
	} else {
		_, err = dbWrapper.Exec(ctx, "UPDATE discovery_detail_checkpoints SET revision=?,body=?,byte_size=? WHERE job_uuid=?", receipt.Revision, string(body), len(body), lease.JobUUID)
	}
	if err != nil {
		return nil, err
	}
	progress, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	if _, err := (&ArchiveJobStore{}).Progress(ctx, lease.ArchiveJobLease, now, progress); err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error { return discoveryDetailEligible(ctx, work, now) })
	*complete = true
	return &receipt, nil
}
