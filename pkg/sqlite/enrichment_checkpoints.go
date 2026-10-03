package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	hexencoding "encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func enrichmentDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hexencoding.EncodeToString(sum[:])
}

func (s *EnrichmentJobStore) CheckpointHead(ctx context.Context, id string) (*models.EnrichmentCheckpoint, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrEnrichmentInvalid
	}
	var row struct {
		models.EnrichmentCheckpointReceipt
		Body string `db:"body"`
	}
	err := dbWrapper.Get(ctx, &row, `SELECT r.*,h.body FROM enrichment_checkpoints h
 JOIN enrichment_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision WHERE h.job_uuid=?`, id)
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

func (s *EnrichmentJobStore) CheckpointReceipts(ctx context.Context, id string, after, limit int) ([]models.EnrichmentCheckpointReceipt, error) {
	if !validSourceRunUUID(id) || after < 0 {
		return nil, models.ErrEnrichmentInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	ret := []models.EnrichmentCheckpointReceipt{}
	err = dbWrapper.Select(ctx, &ret, "SELECT * FROM enrichment_checkpoint_receipts WHERE job_uuid=? AND revision>? ORDER BY revision LIMIT ?", id, after, limit)
	return ret, err
}

func (s *EnrichmentJobStore) CheckpointRecords(ctx context.Context, id string, after, limit int) ([]models.EnrichmentCheckpointRecord, error) {
	if !validSourceRunUUID(id) || after < -1 {
		return nil, models.ErrEnrichmentInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	ret := []models.EnrichmentCheckpointRecord{}
	err = dbWrapper.Select(ctx, &ret, `SELECT r.*,c.fence,a.producer_uuid FROM enrichment_checkpoint_records r
 JOIN enrichment_checkpoint_receipts c ON c.job_uuid=r.job_uuid AND c.revision=r.checkpoint_revision
 JOIN enrichment_job_attempts a ON a.job_uuid=c.job_uuid AND a.fence=c.fence
 WHERE r.job_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?`, id, after, limit)
	return ret, err
}

func (s *EnrichmentJobStore) Checkpoint(ctx context.Context, lease models.EnrichmentJobLease, expected int, raw json.RawMessage, now time.Time) (*models.EnrichmentCheckpointReceipt, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if expected < 0 || !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	if err := s.ownedAttempt(ctx, lease); err != nil {
		return nil, err
	}
	parsed, err := archive.ParseEnrichmentTranscript(raw)
	if err != nil || len(parsed.Records) == 0 || parsed.Schema != archive.EnrichmentTranscriptSchema {
		return nil, models.ErrEnrichmentInvalid
	}
	body := parsed.Body()
	digest := enrichmentDigest(body)
	var receipt models.EnrichmentCheckpointReceipt
	err = dbWrapper.Get(ctx, &receipt, "SELECT * FROM enrichment_checkpoint_receipts WHERE job_uuid=? AND digest=?", lease.JobUUID, digest)
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
	work, err := archive.DecodeEnrichmentJob(current)
	if err != nil {
		return nil, err
	}
	target, err := (&EnrichmentWorkStore{}).Target(ctx, work.TargetUUID)
	if err != nil {
		return nil, err
	}
	if target == nil || parsed.URL != target.URL || parsed.ExtractorVersion != work.ExtractorVersion {
		return nil, models.ErrEnrichmentConflict
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
			return nil, models.ErrEnrichmentConflict
		}
		if now.Before(prior.CreatedAt) {
			return nil, models.ErrEnrichmentInvalid
		}
	}
	if expected != revision {
		return nil, models.ErrEnrichmentConflict
	}
	if revision >= archive.MaxEnrichmentCheckpointRevisions {
		return nil, models.ErrArchiveJobCapacity
	}
	var used int64
	if err := dbWrapper.Get(ctx, &used, "SELECT byte_size FROM enrichment_checkpoint_usage WHERE singleton=1"); err != nil {
		return nil, err
	}
	if int64(len(body)-previousBytes) > archive.MaxEnrichmentStoredBytes-used {
		return nil, models.ErrArchiveJobCapacity
	}
	complete := enrichmentAtomic(ctx)
	receipt = models.EnrichmentCheckpointReceipt{JobUUID: lease.JobUUID, Revision: revision + 1, Digest: digest, Fence: lease.Fence,
		RecordCount: len(parsed.Records), PendingCount: len(parsed.Pending), UnresolvedCount: len(parsed.Unresolved), CreatedAt: now.UTC()}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO enrichment_checkpoint_receipts(job_uuid,revision,digest,fence,record_count,pending_count,unresolved_count,created_at)
 VALUES(?,?,?,?,?,?,?,?)`, receipt.JobUUID, receipt.Revision, receipt.Digest, receipt.Fence, receipt.RecordCount, receipt.PendingCount, receipt.UnresolvedCount, receipt.CreatedAt)
	if err != nil {
		return nil, err
	}
	for ordinal := start; ordinal < receipt.RecordCount; ordinal++ {
		digest, err := parsed.RecordDigest(ordinal)
		if err != nil {
			return nil, err
		}
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO enrichment_checkpoint_records VALUES(?,?,?,?)", lease.JobUUID, ordinal, receipt.Revision, digest); err != nil {
			return nil, err
		}
	}
	if prior == nil {
		_, err = dbWrapper.Exec(ctx, "INSERT INTO enrichment_checkpoints VALUES(?,?,?,?)", lease.JobUUID, receipt.Revision, string(body), len(body))
	} else {
		_, err = dbWrapper.Exec(ctx, "UPDATE enrichment_checkpoints SET revision=?,body=?,byte_size=? WHERE job_uuid=?", receipt.Revision, string(body), len(body), lease.JobUUID)
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
	*complete = true
	return &receipt, nil
}
