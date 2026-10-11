package sqlite

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// ReuseCaptured closes a metadata gap with existing source evidence, preserving
// the original capture and its observation time. A download from another
// collection can fill the same post; an NFO or an unrelated child cannot.
func (s *EnrichmentWorkStore) ReuseCaptured(ctx context.Context, id string, expected int, now time.Time) (*models.EnrichmentCompletion, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	target, err := s.Target(ctx, id)
	if err != nil || target == nil {
		return nil, err
	}
	if target.State == "completed" && target.Revision == expected+1 {
		return s.Completion(ctx, *target.CompletionUUID)
	}
	if expected < 1 || target.Revision != expected || target.State != "pending" {
		return nil, models.ErrEnrichmentConflict
	}
	if err := enrichmentEligible(ctx, target.EnrichmentTargetInput); err != nil {
		return nil, err
	}
	if now.Before(target.NotBefore) {
		return nil, nil
	}
	var protected bool
	if err := dbWrapper.Get(ctx, &protected, `SELECT EXISTS(SELECT 1 FROM enrichment_job_targets t JOIN archive_jobs j ON j.uuid=t.job_uuid
 WHERE t.target_uuid=? AND t.target_revision=? AND (j.state='running'
 OR EXISTS(SELECT 1 FROM enrichment_checkpoints c WHERE c.job_uuid=j.uuid)
 OR EXISTS(SELECT 1 FROM enrichment_job_retained_records c WHERE c.job_uuid=j.uuid)))`, id, expected); err != nil {
		return nil, err
	}
	if protected {
		return nil, nil // An unfinished transcript may contain unresolved children.
	}
	var captures []string
	if err := dbWrapper.Select(ctx, &captures, `SELECT uuid FROM source_captures
 WHERE post_uuid=? AND origin IN ('gallery-dl','gallery-dl-enrichment') ORDER BY captured_at DESC,uuid LIMIT 100`, target.PostUUID); err != nil {
		return nil, err
	}
	for _, captureID := range captures {
		capture, err := (&SourceEvidenceStore{}).FindCapture(ctx, captureID)
		if err != nil {
			return nil, err
		}
		raw, err := archive.RestoreCapture(capture.Payload)
		if err != nil {
			return nil, err
		}
		identity, err := archive.ExtractCapturedPost(raw)
		if err != nil || identity == nil {
			continue
		}
		post, err := (&SourceEvidenceStore{}).FindPostByIdentifier(ctx, *identity)
		if err != nil {
			return nil, err
		}
		if post == nil || post.UUID != target.PostUUID || post.State != "active" {
			continue
		}
		if err := (&SourceCollectionStore{}).RecordCapture(ctx, models.CollectionCapture{CaptureUUID: captureID,
			CollectionUUID: target.CollectionUUID, CollectionRevision: target.CollectionRevision}); err != nil {
			return nil, err
		}
		completion, err := s.Complete(ctx, models.EnrichmentCompletionInput{
			UUID:       uuid.NewSHA1(uuid.MustParse(id), []byte("captured-metadata-reuse-v1")).String(),
			TargetUUID: id, ExpectedRevision: expected, CaptureUUIDs: []string{captureID}}, now)
		if err != nil {
			return nil, err
		}
		// Cancel redundant unstarted work; it did not perform a source fetch.
		binding, err := (&EnrichmentJobStore{}).TargetBinding(ctx, id, expected)
		if err != nil {
			return nil, err
		}
		if binding != nil {
			job, err := (&ArchiveJobStore{}).Find(ctx, binding.JobUUID)
			if err != nil {
				return nil, err
			}
			if job.State == "queued" {
				if _, err := (&ArchiveJobStore{}).Cancel(ctx, job.UUID, job.Revision, now); err != nil {
					return nil, err
				}
			}
		}
		return completion, nil
	}
	return nil, nil
}
