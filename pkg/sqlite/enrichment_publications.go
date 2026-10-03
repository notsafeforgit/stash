package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

const enrichmentPublicationSelect = `SELECT p.*,r.digest,r.record_count,r.unresolved_count,e.capture_count
 FROM enrichment_publications p
 JOIN enrichment_checkpoint_receipts r ON r.job_uuid=p.job_uuid AND r.revision=p.checkpoint_revision
 JOIN enrichment_completions e ON e.uuid=p.completion_uuid`

func (s *EnrichmentJobStore) Publication(ctx context.Context, id string) (*models.EnrichmentPublication, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrEnrichmentInvalid
	}
	ret := &models.EnrichmentPublication{}
	err := dbWrapper.Get(ctx, ret, enrichmentPublicationSelect+" WHERE p.job_uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

const enrichmentPublishedRecordsSelect = `SELECT r.*,c.fence,a.producer_uuid,p.capture_uuid FROM enrichment_published_records p
 JOIN enrichment_checkpoint_records r ON r.job_uuid=p.job_uuid AND r.ordinal=p.ordinal
 JOIN enrichment_checkpoint_receipts c ON c.job_uuid=r.job_uuid AND c.revision=r.checkpoint_revision
 JOIN enrichment_job_attempts a ON a.job_uuid=c.job_uuid AND a.fence=c.fence`

func (s *EnrichmentJobStore) PublishedRecords(ctx context.Context, id string, after, limit int) ([]models.EnrichmentPublishedRecord, error) {
	if !validSourceRunUUID(id) || after < -1 {
		return nil, models.ErrEnrichmentInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	ret := []models.EnrichmentPublishedRecord{}
	err = dbWrapper.Select(ctx, &ret, enrichmentPublishedRecordsSelect+" WHERE p.job_uuid=? AND p.ordinal>? ORDER BY p.ordinal LIMIT ?", id, after, limit)
	return ret, err
}

type enrichmentGet func(any, string, ...any) error
type enrichmentSelect func(any, string, ...any) error

// Compare native storage to the exact source observation, including shared-body
// and profile references. Startup uses the same proof without a writable store.
func verifyEnrichmentCapture(get enrichmentGet, selectRows enrichmentSelect, input models.SourceCaptureInput, post models.SourcePostIdentifier, collection string, revision int) error {
	metadata, revisionSignature, err := canonicalCaptureInput(&input)
	if err != nil {
		return err
	}
	signature, err := captureSignature(input, revisionSignature, sourceDigest(input.Payload.Patch), input.Payload.Refs)
	if err != nil {
		return err
	}
	var row sourceCaptureRow
	if err := get(&row, "SELECT "+sourceCaptureColumns+` FROM source_captures c
 JOIN source_post_revisions r ON r.post_uuid=c.post_uuid AND r.uuid=c.revision_uuid WHERE c.uuid=?`, input.UUID); err != nil {
		return err
	}
	if row.PostUUID != input.PostUUID || row.Origin != input.Origin || row.Platform != input.Platform || !row.CapturedAt.Timestamp.Equal(input.CapturedAt) ||
		!row.ExtractorVersion.Valid || input.ExtractorVersion == nil || row.ExtractorVersion.String != *input.ExtractorVersion || row.RetentionPolicy != input.RetentionPolicy ||
		row.StructureVersion != archive.CaptureStructureVersion || row.Metadata != metadata || row.BodyDigest != sourceDigest(input.Payload.Shared) ||
		row.PatchDigest != sourceDigest(input.Payload.Patch) || row.RevisionSignature != revisionSignature || row.Signature != signature {
		return models.ErrSourcePayloadCorrupt
	}
	var valid bool
	if err := get(&valid, `SELECT EXISTS(SELECT 1 FROM source_post_identifiers WHERE namespace=? AND value=? AND post_uuid=?)
 AND EXISTS(SELECT 1 FROM source_collection_captures c JOIN source_collection_revisions r
  ON r.collection_uuid=c.collection_uuid AND r.revision=c.collection_revision
  WHERE c.capture_uuid=? AND c.collection_uuid=? AND c.collection_revision=? AND (r.namespace='' OR r.namespace=?))`,
		post.Namespace, post.Value, input.PostUUID, input.UUID, collection, revision, post.Namespace); err != nil {
		return err
	}
	if !valid {
		return models.ErrEnrichmentConflict
	}
	refs := []models.SourceProfileReference{}
	if err := selectRows(&refs, "SELECT part,path,profile_hash AS hash FROM source_capture_profiles WHERE capture_uuid=? ORDER BY part,path LIMIT 1025", input.UUID); err != nil {
		return err
	}
	if !reflect.DeepEqual(refs, input.Payload.Refs) {
		return models.ErrSourcePayloadCorrupt
	}
	for _, profile := range input.Payload.Profiles {
		if err := get(&valid, "SELECT EXISTS(SELECT 1 FROM source_profile_bodies WHERE hash=? AND namespace=? AND payload_digest=?)", profile.Hash, profile.Namespace, sourceDigest(profile.Body)); err != nil {
			return err
		}
		if !valid {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return nil
}

func enrichmentPublishedJob(ctx context.Context, id string) (*models.EnrichmentPublication, error) {
	publication, err := (&EnrichmentJobStore{}).Publication(ctx, id)
	if err != nil {
		return nil, err
	}
	job, err := (&ArchiveJobStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if publication == nil || job == nil || job.State != "succeeded" || job.Fence != publication.Fence {
		return nil, models.ErrEnrichmentAtomic
	}
	result, err := archive.EnrichmentPublicationResult(*publication)
	if err != nil || !bytes.Equal(result, job.Result) {
		return nil, models.ErrEnrichmentAtomic
	}
	return publication, nil
}

func enrichmentJobSuccessGuard(ctx context.Context, id string) {
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		_, err := enrichmentPublishedJob(ctx, id)
		return err
	})
}

func (s *EnrichmentJobStore) Publish(ctx context.Context, lease models.EnrichmentJobLease, expected int, digest string, captures []string, now time.Time) (*models.EnrichmentPublication, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if expected < 1 || !archive.ValidSHA256(digest) || !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	if err := s.ownedAttempt(ctx, lease); err != nil {
		return nil, err
	}
	prior, err := s.Publication(ctx, lease.JobUUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.CheckpointRevision != expected || prior.Digest != digest || prior.Fence != lease.Fence {
			return nil, models.ErrEnrichmentConflict
		}
		return enrichmentPublishedJob(ctx, lease.JobUUID)
	}
	job, err := s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	work, err := archive.DecodeEnrichmentJob(job)
	if err != nil {
		return nil, err
	}
	head, err := s.CheckpointHead(ctx, job.UUID)
	if err != nil {
		return nil, err
	}
	if head == nil || head.Revision != expected || head.Digest != digest || head.PendingCount != 0 || head.RecordCount != len(captures) || now.Before(head.CreatedAt) {
		return nil, models.ErrEnrichmentConflict
	}
	transcript, err := archive.ParseEnrichmentTranscript(head.Body)
	if err != nil {
		return nil, err
	}
	get := func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) }
	selectRows := func(out any, query string, args ...any) error { return dbWrapper.Select(ctx, out, query, args...) }
	unique := make(map[string]bool)
	for after := -1; after+1 < len(captures); {
		records, err := s.CheckpointRecords(ctx, job.UUID, after, 100)
		if err != nil {
			return nil, err
		}
		if len(records) == 0 {
			return nil, models.ErrSourcePayloadCorrupt
		}
		for _, record := range records {
			if record.Ordinal != after+1 || record.Ordinal >= len(captures) {
				return nil, models.ErrSourcePayloadCorrupt
			}
			input, post, err := archive.PrepareEnrichmentCapture(job.UUID, work, transcript, record)
			if err != nil {
				return nil, err
			}
			if input.UUID != captures[record.Ordinal] {
				return nil, models.ErrEnrichmentConflict
			}
			if err := verifyEnrichmentCapture(get, selectRows, *input, *post, work.CollectionUUID, work.CollectionRevision); err != nil {
				return nil, err
			}
			unique[input.UUID], after = true, record.Ordinal
		}
	}
	complete := enrichmentAtomic(ctx)
	completionID, err := archive.EnrichmentCompletionUUID(job.UUID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	_, err = (&EnrichmentWorkStore{}).Complete(ctx, models.EnrichmentCompletionInput{UUID: completionID, TargetUUID: work.TargetUUID, ExpectedRevision: work.TargetRevision, CaptureUUIDs: ids}, now)
	if err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, "INSERT INTO enrichment_publications(job_uuid,checkpoint_revision,fence,completion_uuid,created_at) VALUES(?,?,?,?,?)", job.UUID, expected, lease.Fence, completionID, now.UTC())
	if err != nil {
		return nil, err
	}
	for ordinal, id := range captures {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO enrichment_published_records VALUES(?,?,?)", job.UUID, ordinal, id); err != nil {
			return nil, err
		}
	}
	publication, err := s.Publication(ctx, job.UUID)
	if err != nil {
		return nil, err
	}
	result, err := archive.EnrichmentPublicationResult(*publication)
	if err != nil {
		return nil, err
	}
	if _, err := (&ArchiveJobStore{}).Finish(ctx, lease.ArchiveJobLease, now, models.ArchiveJobOutcome{State: "succeeded", Result: result}); err != nil {
		return nil, err
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		if _, err := enrichmentPublishedJob(ctx, job.UUID); err != nil {
			return err
		}
		target, err := (&EnrichmentWorkStore{}).Target(ctx, work.TargetUUID)
		if err != nil {
			return err
		}
		if target == nil || target.State != "completed" || target.Revision != work.TargetRevision+1 || target.CompletionUUID == nil || *target.CompletionUUID != completionID {
			return models.ErrEnrichmentAtomic
		}
		return enrichmentSourceEligible(ctx, work, target)
	})
	*complete = true
	return publication, nil
}
