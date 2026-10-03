package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

func readEnrichmentCheckpointRelease(get enrichmentGet, id string) (*models.EnrichmentCheckpointRelease, error) {
	var row struct {
		models.EnrichmentCheckpointRelease
		References string `db:"unresolved"`
	}
	err := get(&row, "SELECT * FROM enrichment_checkpoint_releases WHERE job_uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(row.References) > archive.MaxSourcePayloadBytes || json.Unmarshal([]byte(row.References), &row.Unresolved) != nil || row.Unresolved == nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	canonical, err := json.Marshal(row.Unresolved)
	if err != nil || string(canonical) != row.References || len(row.Unresolved) > archive.MaxEnrichmentReferences ||
		(row.Version != 1 && row.Version != 2) || !archive.ValidSHA256(row.ProofSHA256) || !validJobTime(row.CreatedAt) ||
		row.CheckpointBytes < 1 || row.CheckpointBytes > archive.MaxEnrichmentTranscriptBytes {
		return nil, models.ErrSourcePayloadCorrupt
	}
	row.CreatedAt = row.CreatedAt.UTC()
	return &row.EnrichmentCheckpointRelease, nil
}

func (s *EnrichmentJobStore) CheckpointRelease(ctx context.Context, id string) (*models.EnrichmentCheckpointRelease, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrEnrichmentInvalid
	}
	return readEnrichmentCheckpointRelease(func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) }, id)
}

// The seal binds immutable acknowledgement/provenance rows to fully validated
// native captures and unresolved references. It deliberately does not duplicate
// source payloads or claim to retain the compact transcript's encoding choices.
func enrichmentReleaseProof(get enrichmentGet, selectRows enrichmentSelect, publication models.EnrichmentPublication, release models.EnrichmentCheckpointRelease) (string, error) {
	var row archiveJobRow
	if err := get(&row, "SELECT * FROM archive_jobs WHERE uuid=?", publication.JobUUID); err != nil {
		return "", err
	}
	job := row.resolve()
	work, err := archive.DecodeEnrichmentJob(job)
	if err != nil {
		return "", err
	}
	result, err := archive.EnrichmentPublicationResult(publication)
	completion, completionErr := archive.EnrichmentCompletionUUID(job.UUID)
	if err != nil || completionErr != nil || completion != publication.CompletionUUID || !bytes.Equal(result, job.Result) ||
		job.State != "succeeded" || job.Fence != publication.Fence || publication.CreatedAt.UnixMilli() != job.UpdatedAt.UnixMilli() ||
		release.Version != work.Version || release.JobUUID != job.UUID || release.CreatedAt.Before(publication.CreatedAt) || len(release.Unresolved) != publication.UnresolvedCount {
		return "", models.ErrSourcePayloadCorrupt
	}
	var receipts []models.EnrichmentCheckpointReceipt
	if err := selectRows(&receipts, "SELECT * FROM enrichment_checkpoint_receipts WHERE job_uuid=? ORDER BY revision LIMIT 129", job.UUID); err != nil {
		return "", err
	}
	if len(receipts) != publication.CheckpointRevision || len(receipts) < 1 || len(receipts) > archive.MaxEnrichmentCheckpointRevisions {
		return "", models.ErrSourcePayloadCorrupt
	}
	for i := range receipts {
		if receipts[i].Revision != i+1 || !archive.ValidSHA256(receipts[i].Digest) || !validJobTime(receipts[i].CreatedAt) {
			return "", models.ErrSourcePayloadCorrupt
		}
		receipts[i].CreatedAt = receipts[i].CreatedAt.UTC()
	}
	last := receipts[len(receipts)-1]
	if last.Digest != publication.Digest || last.PendingCount != 0 || last.RecordCount != publication.RecordCount || last.UnresolvedCount != publication.UnresolvedCount {
		return "", models.ErrSourcePayloadCorrupt
	}
	var records []models.EnrichmentPublishedRecord
	if err := selectRows(&records, enrichmentPublishedRecordsSelect+" WHERE p.job_uuid=? ORDER BY p.ordinal LIMIT 1025", job.UUID); err != nil {
		return "", err
	}
	if len(records) != publication.RecordCount || len(records) < 1 || len(records) > archive.MaxEnrichmentCaptures {
		return "", models.ErrSourcePayloadCorrupt
	}
	var seed *archive.EnrichmentTranscript
	if work.Handoff != nil {
		value, err := readEnrichmentSeed(get, selectRows, job, work)
		if err != nil {
			return "", err
		}
		if value == nil {
			return "", models.ErrSourcePayloadCorrupt
		}
		seed, err = archive.ParseEnrichmentTranscript(value.Body)
		if err != nil {
			return "", err
		}
		if len(records) < len(seed.Records) {
			return "", models.ErrSourcePayloadCorrupt
		}
	}
	signatures := make(map[string]string)
	preceding := make(map[string]bool)
	for i, record := range records {
		if record.Ordinal != i || !archive.ValidSHA256(record.Digest) || record.CheckpointRevision < 1 || record.CheckpointRevision > len(receipts) ||
			record.Fence != receipts[record.CheckpointRevision-1].Fence || !validSourceRunUUID(record.ProducerUUID) {
			return "", models.ErrSourcePayloadCorrupt
		}
		if _, ok := signatures[record.CaptureUUID]; ok && work.Version == 1 {
			continue
		}
		capture, err := findSourceCapture(get, selectRows, record.CaptureUUID)
		if err != nil {
			return "", err
		}
		retained := seed != nil && i < len(seed.Records)
		if capture == nil || capture.PostUUID != work.PostUUID {
			return "", models.ErrSourcePayloadCorrupt
		}
		if retained {
			digest, err := seed.RecordDigest(i)
			if err != nil || digest != record.Digest || seed.Records[i].RetainedCapture == nil || *seed.Records[i].RetainedCapture != capture.UUID ||
				capture.Origin != "legacy-enrichment" || capture.RetentionPolicy != "legacy-retained-v1" || !capture.CapturedAt.IsZero() || capture.RecordedAt == nil {
				return "", models.ErrSourcePayloadCorrupt
			}
			records[i].RetainedCapture = seed.Records[i].RetainedCapture
		} else if capture.Origin != "gallery-dl" || capture.ExtractorVersion == nil || *capture.ExtractorVersion != work.ExtractorVersion ||
			(capture.RetentionPolicy != archive.SourceRetentionVersion && (work.Version != 2 || capture.RetentionPolicy != archive.CaptureContextPolicy)) {
			return "", models.ErrSourcePayloadCorrupt
		}
		raw, err := archive.RestoreCapture(capture.Payload)
		if err != nil {
			return "", err
		}
		post, err := archive.ExtractCapturedPost(raw)
		if err != nil || post == nil {
			return "", models.ErrSourcePayloadCorrupt
		}
		if work.Version == 2 && !retained {
			parent := ""
			if len(capture.Contexts) != 0 {
				if len(capture.Contexts) != 1 || !preceding[capture.Contexts[0].ParentUUID] {
					return "", models.ErrSourcePayloadCorrupt
				}
				parent = capture.Contexts[0].ParentUUID
			}
			if capture.CapturedAt.IsZero() || capture.RecordedAt != nil || archive.ContextEnrichmentCaptureUUID(job.UUID, record.ProducerUUID, capture.CapturedAt, raw, parent) != capture.UUID {
				return "", models.ErrSourcePayloadCorrupt
			}
		}
		input := models.SourceCaptureInput{UUID: capture.UUID, PostUUID: capture.PostUUID, Origin: capture.Origin, Platform: capture.Platform,
			CapturedAt: capture.CapturedAt, RecordedAt: capture.RecordedAt, Contexts: capture.Contexts, ExtractorVersion: capture.ExtractorVersion, RetentionPolicy: capture.RetentionPolicy, Metadata: capture.Metadata, Payload: *capture.Payload}
		if err := verifyEnrichmentCapture(get, selectRows, input, *post, work.CollectionUUID, work.CollectionRevision); err != nil {
			return "", err
		}
		var signature string
		if err := get(&signature, "SELECT signature FROM source_captures WHERE uuid=?", capture.UUID); err != nil {
			return "", err
		}
		signatures[capture.UUID] = signature
		preceding[capture.UUID] = true
	}
	if len(signatures) != publication.CaptureCount {
		return "", models.ErrSourcePayloadCorrupt
	}
	for _, ref := range release.Unresolved {
		if ref.Parent < 0 || ref.Parent >= len(records) || ref.Depth < 1 || ref.Depth > 3 {
			return "", models.ErrSourcePayloadCorrupt
		}
	}
	// Freeze the v1 checksum projection separately from evolving API structs.
	// Adding an API field must not invalidate already released checkpoints.
	receiptProof := make([]any, 0, len(receipts))
	for _, receipt := range receipts {
		receiptProof = append(receiptProof, []any{receipt.Revision, receipt.Digest, receipt.Fence, receipt.RecordCount,
			receipt.PendingCount, receipt.UnresolvedCount, receipt.CreatedAt.UTC().Format(time.RFC3339Nano)})
	}
	recordProof := make([]any, 0, len(records))
	for _, record := range records {
		entry := []any{record.Ordinal, record.CheckpointRevision, record.Digest, record.Fence, record.ProducerUUID, record.CaptureUUID}
		if work.Version == 2 {
			entry = append(entry, record.RetainedCapture)
		}
		recordProof = append(recordProof, entry)
	}
	references := make([]any, 0, len(release.Unresolved))
	for _, ref := range release.Unresolved {
		references = append(references, []any{ref.URL, ref.Parent, ref.Depth, ref.Reason})
	}
	domain := "stash-enrichment-checkpoint-release-v1"
	arguments := []any{work.Version, work.TargetUUID, work.TargetRevision, work.PostUUID, work.CollectionUUID, work.CollectionRevision, work.RootUUID, work.PolicySHA256, work.ExtractorVersion}
	if work.Version == 2 {
		domain = "stash-enrichment-checkpoint-release-v2"
		var handoff any
		if work.Handoff != nil {
			handoff = []any{work.Handoff.UUID, work.Handoff.PlanSHA256, work.Handoff.SeedSHA256}
		}
		arguments = append(arguments, work.CapturePolicy, handoff)
	}
	return sourceSignature(domain, []any{
		arguments,
		[]any{publication.JobUUID, publication.CheckpointRevision, publication.Digest, publication.Fence, publication.CompletionUUID,
			publication.RecordCount, publication.CaptureCount, publication.UnresolvedCount, publication.CreatedAt.UTC().Format(time.RFC3339Nano)},
		receiptProof, recordProof, signatures,
		[]any{release.Version, release.CheckpointBytes, release.CreatedAt.UTC().Format(time.RFC3339Nano), references},
	})
}

func (s *EnrichmentJobStore) ReleaseCheckpoint(ctx context.Context, id string, now time.Time) (*models.EnrichmentCheckpointRelease, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	publication, err := enrichmentPublishedJob(ctx, id)
	if err != nil {
		return nil, err
	}
	get := func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) }
	selectRows := func(out any, query string, args ...any) error { return dbWrapper.Select(ctx, out, query, args...) }
	prior, err := readEnrichmentCheckpointRelease(get, id)
	if err != nil {
		return nil, err
	}
	head, err := s.CheckpointHead(ctx, id)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		proof, err := enrichmentReleaseProof(get, selectRows, *publication, *prior)
		if err != nil {
			return nil, err
		}
		if head != nil || proof != prior.ProofSHA256 {
			return nil, models.ErrSourcePayloadCorrupt
		}
		return prior, nil
	}
	if head == nil || head.Revision != publication.CheckpointRevision || head.Digest != publication.Digest || now.Before(publication.CreatedAt) {
		return nil, models.ErrEnrichmentConflict
	}
	transcript, err := verifyEnrichmentPublicationCheckpoint(get, selectRows, *publication, head.Body)
	if err != nil {
		return nil, err
	}
	job, err := (&ArchiveJobStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	work, err := archive.DecodeEnrichmentJob(job)
	if err != nil {
		return nil, err
	}
	release := models.EnrichmentCheckpointRelease{JobUUID: id, Version: work.Version, CheckpointBytes: len(head.Body), CreatedAt: now.UTC(), Unresolved: transcript.Unresolved}
	release.ProofSHA256, err = enrichmentReleaseProof(get, selectRows, *publication, release)
	if err != nil {
		return nil, err
	}
	references, err := json.Marshal(release.Unresolved)
	if err != nil {
		return nil, err
	}
	complete := enrichmentAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, "INSERT INTO enrichment_checkpoint_releases(job_uuid,version,proof_sha256,checkpoint_bytes,unresolved,created_at) VALUES(?,?,?,?,?,?)",
		id, release.Version, release.ProofSHA256, release.CheckpointBytes, string(references), release.CreatedAt)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "DELETE FROM enrichment_checkpoints WHERE job_uuid=?", id); err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		var intact bool
		if err := dbWrapper.Get(ctx, &intact, `SELECT EXISTS(SELECT 1 FROM enrichment_checkpoint_releases WHERE job_uuid=? AND proof_sha256=?)
 AND NOT EXISTS(SELECT 1 FROM enrichment_checkpoints WHERE job_uuid=?)`, id, release.ProofSHA256, id); err != nil {
			return err
		}
		if !intact {
			return models.ErrEnrichmentAtomic
		}
		return nil
	})
	*complete = true
	return &release, nil
}
