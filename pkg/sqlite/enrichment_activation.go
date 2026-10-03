package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func enrichmentActivationSnapshot(ctx context.Context, snapshot, expected string) error {
	if snapshot == "" {
		return nil
	}
	progress, err := (&AutomationEnrichmentImportStore{}).Find(ctx, snapshot)
	if err != nil {
		return err
	}
	if progress == nil || progress.State == "running" || progress.ManifestSHA256 != expected {
		return models.ErrEnrichmentConflict
	}
	return nil
}

func enrichmentActivationSourceTarget(ctx context.Context, input models.EnrichmentActivationInput, ref models.EnrichmentTargetRef) error {
	if input.SnapshotUUID == "" {
		return nil
	}
	var found bool
	err := dbWrapper.Get(ctx, &found, `SELECT EXISTS(SELECT 1 FROM automation_enrichment_records
 WHERE snapshot_uuid=? AND target_uuid=? AND target_revision=? AND disposition='held')`, input.SnapshotUUID, ref.TargetUUID, ref.Revision)
	if err != nil {
		return err
	}
	if !found {
		return models.ErrEnrichmentConflict
	}
	return nil
}

func (s *EnrichmentWorkStore) PreviewActivation(ctx context.Context, input models.EnrichmentActivationInput) (*models.EnrichmentActivationPlan, error) {
	input, _, err := archive.PrepareEnrichmentActivation(input)
	if err != nil {
		return nil, err
	}
	if err := enrichmentActivationSnapshot(ctx, input.SnapshotUUID, input.ManifestSHA256); err != nil {
		return nil, err
	}
	ret := &models.EnrichmentActivationPlan{Version: 1, Input: input}
	released := map[string]bool{}
	for _, ref := range input.Targets {
		if err := enrichmentActivationSourceTarget(ctx, input, ref.EnrichmentTargetRef); err != nil {
			return nil, err
		}
		target, err := s.Target(ctx, ref.TargetUUID)
		if err != nil {
			return nil, err
		}
		if target == nil || target.Revision != ref.Revision || target.State != "held" {
			return nil, models.ErrEnrichmentConflict
		}
		destination := target.EnrichmentTargetInput
		destination.CollectionRevision = ref.CollectionRevision
		if err := enrichmentEligible(ctx, destination); err != nil {
			return nil, err
		}
		post, err := activePostLink(ctx, target.PostUUID)
		if err != nil {
			return nil, err
		}
		entry := models.EnrichmentActivationEntry{EnrichmentTargetRef: ref.EnrichmentTargetRef, ActivationCollectionRevision: ref.CollectionRevision,
			PostUUID: target.PostUUID, PostRevision: post.Revision, URLUUID: target.URLUUID, URL: target.URL,
			CollectionUUID: target.CollectionUUID, CollectionRevision: target.CollectionRevision,
			Policy: target.Policy, Priority: target.Priority, NotBefore: target.NotBefore}
		if err := enrichmentActivationDestination(&entry); err != nil {
			return nil, err
		}
		if released[entry.ReleasedTargetUUID] {
			return nil, models.ErrEnrichmentConflict
		}
		released[entry.ReleasedTargetUUID] = true
		if entry.ReleasedTargetUUID != entry.TargetUUID {
			prior, err := s.Target(ctx, entry.ReleasedTargetUUID)
			if err != nil {
				return nil, err
			}
			if prior != nil {
				return nil, models.ErrEnrichmentConflict
			}
		}
		ret.Entries = append(ret.Entries, entry)
	}
	ret.PlanSHA256, err = archive.EnrichmentActivationDigest(*ret)
	return ret, err
}

func (s *EnrichmentWorkStore) Activation(ctx context.Context, id string) (*models.EnrichmentActivation, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrEnrichmentInvalid
	}
	var row enrichmentActivationRecord
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM enrichment_activations WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plan, err := row.decode()
	if err != nil {
		return nil, err
	}
	ret := &models.EnrichmentActivation{EnrichmentActivationPlan: *plan, CreatedAt: row.CreatedAt}
	err = dbWrapper.Select(ctx, &ret.Activated, `SELECT released_target_uuid AS target_uuid,released_revision AS target_revision
 FROM enrichment_activation_targets WHERE activation_uuid=? ORDER BY enrichment_activation_targets.target_uuid`, id)
	if err != nil {
		return nil, err
	}
	if len(ret.Activated) != len(plan.Input.Targets) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	for i, ref := range ret.Activated {
		entry := plan.Entries[i]
		if ref.TargetUUID != entry.ReleasedTargetUUID || ref.Revision != entry.ReleasedRevision {
			return nil, models.ErrSourcePayloadCorrupt
		}
	}
	return ret, nil
}

func (s *EnrichmentWorkStore) Activate(ctx context.Context, input models.EnrichmentActivationInput, expected string, now time.Time) (*models.EnrichmentActivation, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	input, inputSHA, err := archive.PrepareEnrichmentActivation(input)
	if err != nil || !archive.ValidSHA256(expected) || !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	prior, err := s.Activation(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		_, priorSHA, err := archive.PrepareEnrichmentActivation(prior.Input)
		if err != nil || priorSHA != inputSHA || prior.PlanSHA256 != expected {
			return nil, models.ErrEnrichmentConflict
		}
		return prior, nil
	}
	plan, err := s.PreviewActivation(ctx, input)
	if err != nil {
		return nil, err
	}
	if plan.PlanSHA256 != expected {
		return nil, models.ErrEnrichmentConflict
	}
	complete := enrichmentAtomic(ctx)
	for _, entry := range plan.Entries {
		schedule := models.EnrichmentSchedule{State: "pending", Priority: entry.Priority, NotBefore: entry.NotBefore}
		if entry.ReleasedTargetUUID == entry.TargetUUID {
			if _, err := s.Schedule(ctx, entry.TargetUUID, entry.Revision, schedule, now); err != nil {
				return nil, err
			}
		} else {
			// Consume the reviewed historical hold and retain an exact replacement
			// for the active collection revision. The old scope stays inspectable.
			if _, err := s.Schedule(ctx, entry.TargetUUID, entry.Revision,
				models.EnrichmentSchedule{State: "excluded", Priority: entry.Priority, NotBefore: entry.NotBefore, Reason: "activation_rebound"}, now); err != nil {
				return nil, err
			}
			if _, err := s.RetainTarget(ctx, models.EnrichmentTargetInput{PostUUID: entry.PostUUID, URLUUID: entry.URLUUID,
				CollectionUUID: entry.CollectionUUID, CollectionRevision: entry.ActivationCollectionRevision, Policy: entry.Policy, Origin: "review"}, schedule, now); err != nil {
				return nil, err
			}
		}
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	var snapshot, manifest any
	if input.SnapshotUUID != "" {
		snapshot, manifest = input.SnapshotUUID, input.ManifestSHA256
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO enrichment_activations(uuid,input_sha256,plan_sha256,snapshot_uuid,manifest_sha256,plan,created_at) VALUES(?,?,?,?,?,?,?)`,
		input.UUID, inputSHA, expected, snapshot, manifest, string(body), now.UTC())
	if err != nil {
		return nil, err
	}
	for _, entry := range plan.Entries {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO enrichment_activation_targets(activation_uuid,target_uuid,previous_revision,consumed_revision,released_target_uuid,released_revision) VALUES(?,?,?,?,?,?)`,
			input.UUID, entry.TargetUUID, entry.Revision, entry.Revision+1, entry.ReleasedTargetUUID, entry.ReleasedRevision); err != nil {
			return nil, err
		}
	}
	ret, err := s.Activation(ctx, input.UUID)
	*complete = err == nil
	return ret, err
}

func (s *AutomationEnrichmentImportStore) HeldTargets(ctx context.Context, id, expected string, after int64, limit int) ([]models.AutomationEnrichmentCandidate, error) {
	if !validSourceRunUUID(id) || !archive.ValidSHA256(expected) || after < 0 || limit < 1 || limit > archive.MaxEnrichmentActivationTargets {
		return nil, models.ErrEnrichmentInvalid
	}
	if err := enrichmentActivationSnapshot(ctx, id, expected); err != nil {
		return nil, err
	}
	ret := []models.AutomationEnrichmentCandidate{}
	// Two legacy aliases can map to the same target, including the same hold
	// revision. Emit only its last original hold so paging cannot release it twice.
	err := dbWrapper.Select(ctx, &ret, `SELECT r.ordinal,r.target_uuid,r.target_revision,t.revision AS current_revision,t.state,p.state AS post_state,
 t.post_uuid,p.revision AS post_revision,t.url_uuid,u.url,t.collection_uuid,t.collection_revision,t.policy,t.priority,t.not_before,
 c.revision AS current_collection_revision,d.state AS collection_state
 FROM automation_enrichment_records r INDEXED BY automation_enrichment_held
 JOIN enrichment_targets t ON t.uuid=r.target_uuid JOIN source_posts p ON p.uuid=t.post_uuid
 JOIN source_post_urls u ON u.uuid=t.url_uuid JOIN source_collections c ON c.uuid=t.collection_uuid
 JOIN source_collection_revisions d ON d.collection_uuid=c.uuid AND d.revision=c.revision
 WHERE r.snapshot_uuid=? AND r.ordinal>? AND r.disposition='held'
 AND r.ordinal=(SELECT max(h.ordinal) FROM automation_enrichment_records h INDEXED BY automation_enrichment_held_targets
  WHERE h.snapshot_uuid=r.snapshot_uuid AND h.target_uuid=r.target_uuid AND h.disposition='held')
 ORDER BY r.ordinal LIMIT ?`, id, after, limit)
	if err != nil {
		return nil, err
	}
	for i := range ret {
		row := &ret[i]
		row.ActivationCollectionRevision = row.CurrentCollectionRevision
		if err := enrichmentActivationDestination(&row.EnrichmentActivationEntry); err != nil {
			return nil, err
		}
		switch {
		case row.PostState != "active":
			row.Disposition = "post_forgotten"
		case row.State == "completed":
			row.Disposition = "completed"
		case row.CollectionState == "retired":
			row.Disposition = "collection_retired"
		case row.CollectionState != "active":
			row.Disposition = "collection_disabled"
		case row.State != "held" || row.CurrentRevision != row.Revision:
			row.Disposition = "changed"
		default:
			row.Disposition = "eligible"
			if row.ReleasedTargetUUID != row.TargetUUID {
				replacement, err := (&EnrichmentWorkStore{}).Target(ctx, row.ReleasedTargetUUID)
				if err != nil {
					return nil, err
				}
				if replacement != nil {
					row.Disposition = "replacement_exists"
				}
			}
		}
	}
	return ret, nil
}

func enrichmentActivationDestination(entry *models.EnrichmentActivationEntry) error {
	id, err := archive.EnrichmentTargetIdentity(models.EnrichmentTargetInput{PostUUID: entry.PostUUID, URLUUID: entry.URLUUID,
		CollectionUUID: entry.CollectionUUID, CollectionRevision: entry.ActivationCollectionRevision, Policy: entry.Policy, Origin: "review"})
	if err != nil {
		return err
	}
	entry.ReleasedTargetUUID, entry.ReleasedRevision = id, 1
	if id == entry.TargetUUID {
		entry.ReleasedRevision = entry.Revision + 1
	}
	return nil
}
