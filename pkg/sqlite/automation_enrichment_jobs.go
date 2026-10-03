package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func (w *automationEnrichmentWork) job(ctx context.Context, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord, now time.Time) (*models.AutomationEnrichmentRecord, error) {
	r := &models.AutomationEnrichmentRecord{Ordinal: row.Ordinal, Outcome: "mapped"}
	job, reason := scrape.PrepareAutomationEnrichmentJob(record.Values, w.captured)
	if reason != "" {
		return automationEnrichmentReview(r, reason), nil
	}
	r.HistoricalAttempts, r.ServiceScope, r.StagedSHA256 = &job.Attempts, job.ServiceScope, job.StagedSHA256
	if reason, err := w.post(ctx, job.CatalogID, job.PostKey, r); err != nil {
		return nil, err
	} else if reason != "" {
		return automationEnrichmentReview(r, reason), nil
	}
	cooldowns, unresolvedCooldowns, err := w.cooldowns(ctx, job)
	if err != nil {
		return nil, err
	}
	deadline := scrape.EnrichmentNotBefore(*job, cooldowns, time.Time{})
	r.NotBefore = &deadline
	r.Disposition = job.Disposition
	switch job.Disposition {
	case "historical_completion":
		key, err := archive.EncodeSourceJSON([]any{job.PostKey, job.Version})
		if err != nil {
			return nil, err
		}
		var receipt string
		err = dbWrapper.Get(ctx, &receipt, `SELECT x.uuid FROM catalog_enrichment_records r
 JOIN source_enrichment_receipts x ON x.uuid=r.receipt_uuid
 JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 WHERE r.snapshot_uuid=? AND r.outcome='mapped' AND x.post_uuid=? AND x.collection_uuid=? AND x.collection_revision=1
 ORDER BY (e.source_key=?) DESC,x.uuid LIMIT 1`, *r.CatalogSnapshotUUID, *r.PostUUID, *r.CollectionUUID, string(key))
		if errors.Is(err, sql.ErrNoRows) {
			return automationEnrichmentReview(r, "historical_completion_requires_evidence"), nil
		}
		if err != nil {
			return nil, err
		}
		r.ReceiptUUID = &receipt
	case "source_present":
		var capture string
		err = dbWrapper.Get(ctx, &capture, `SELECT c.uuid FROM source_captures c JOIN source_collection_captures b ON b.capture_uuid=c.uuid
 WHERE c.post_uuid=? AND c.origin='gallery-dl' AND b.collection_uuid=? AND b.collection_revision=1 ORDER BY c.uuid LIMIT 1`, *r.PostUUID, *r.CollectionUUID)
		if errors.Is(err, sql.ErrNoRows) {
			return automationEnrichmentReview(r, "existing_source_requires_evidence"), nil
		}
		if err != nil {
			return nil, err
		}
		r.CaptureUUID = &capture
	case "coalesced":
		return w.coalesced(ctx, job, r)
	case "review":
		automationEnrichmentReview(r, "legacy_"+job.Status)
	case "staged_review":
		r.Outcome, r.Reason = "review", "legacy_checkpoint_conversion"
	case "held":
		if unresolvedCooldowns {
			automationEnrichmentReview(r, "legacy_cooldowns_require_review")
		}
	}
	// A missing/unsupported historical URL stays original evidence. It cannot
	// be replaced with a guessed URL merely to manufacture an execution target.
	if job.URL == nil || !archive.EnrichmentPostURL(*job.URL) {
		return r, nil
	}
	if err := w.queueURL(ctx, *job.URL, r); err != nil {
		return nil, err
	}
	input := models.EnrichmentTargetInput{PostUUID: *r.PostUUID, URLUUID: *r.URLUUID, CollectionUUID: *r.CollectionUUID,
		CollectionRevision: 1, Policy: models.EnrichmentGalleryMetadataV1, Origin: "migration"}
	id, err := archive.EnrichmentTargetIdentity(input)
	if err != nil {
		return nil, err
	}
	store := &EnrichmentWorkStore{}
	target, err := store.Target(ctx, id)
	if err != nil {
		return nil, err
	}
	owned := false
	if target != nil {
		if target.Origin == "migration" && target.State == "held" {
			err = dbWrapper.Get(ctx, &owned, `SELECT EXISTS(SELECT 1 FROM automation_enrichment_records
 WHERE snapshot_uuid=? AND target_uuid=? AND target_revision=? AND disposition='held')`, w.snapshot.UUID, target.UUID, target.Revision)
			if err != nil {
				return nil, err
			}
		}
		if !owned {
			r.TargetUUID, r.TargetRevision = &target.UUID, &target.Revision
			if r.Outcome == "mapped" {
				r.Disposition = "preserved"
			}
			return r, nil
		}
	}
	state, targetReason := "held", ""
	if r.Outcome == "review" {
		state, targetReason = "review", r.Reason
	} else if job.Disposition == "excluded" {
		state, targetReason = "excluded", "legacy_excluded_source"
	}
	priority := job.Priority
	if owned {
		deadline = scrape.EnrichmentNotBefore(*job, cooldowns, target.NotBefore)
		priority = max(priority, target.Priority)
	}
	schedule := models.EnrichmentSchedule{State: state, Priority: priority, NotBefore: deadline, Reason: targetReason}
	if target == nil {
		target, err = store.RetainTarget(ctx, input, schedule, now)
	} else {
		target, err = store.Schedule(ctx, target.UUID, target.Revision, schedule, now)
	}
	if err != nil {
		return nil, err
	}
	if r.Outcome == "mapped" && (r.ReceiptUUID != nil || r.CaptureUUID != nil) {
		target, err = completeImportedEnrichment(ctx, target, r.ReceiptUUID, r.CaptureUUID, now)
		if err != nil {
			return nil, err
		}
		r.CompletionUUID = target.CompletionUUID
	}
	r.TargetUUID, r.TargetRevision = &target.UUID, &target.Revision
	return r, nil
}

func (w *automationEnrichmentWork) coalesced(ctx context.Context, job *scrape.AutomationEnrichmentJob, r *models.AutomationEnrichmentRecord) (*models.AutomationEnrichmentRecord, error) {
	var state string
	err := dbWrapper.Get(ctx, &state, "SELECT state FROM catalog_relations_imports WHERE snapshot_uuid=?", *r.CatalogSnapshotUUID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && state == "running") {
		return nil, models.ErrAutomationSnapshotConflict
	}
	if err != nil {
		return nil, err
	}
	key, err := archive.EncodeSourceJSON([]string{job.PostKey})
	if err != nil {
		return nil, err
	}
	var ordinal int64
	err = dbWrapper.Get(ctx, &ordinal, `SELECT e.ordinal FROM catalog_snapshot_records e
 JOIN catalog_relation_records r ON r.snapshot_uuid=e.snapshot_uuid AND r.ordinal=e.ordinal
 WHERE e.snapshot_uuid=? AND e.source_table='post_aliases' AND e.source_key=? AND r.post_uuid=? AND r.outcome='mapped'`, *r.CatalogSnapshotUUID, string(key), *r.PostUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return automationEnrichmentReview(r, "coalesced_post_requires_alias"), nil
	}
	if err != nil {
		return nil, err
	}
	r.AliasOrdinal = &ordinal
	return r, nil
}
