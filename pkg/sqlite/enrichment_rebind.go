package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func (s *EnrichmentWorkStore) rebindCandidate(ctx context.Context, target *models.EnrichmentTarget, collection *models.SourceCollection) (*models.EnrichmentRebindCandidate, error) {
	ret := &models.EnrichmentRebindCandidate{Target: *target, CurrentCollectionRevision: collection.Revision, CollectionState: collection.State}
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, target.PostUUID)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	ret.PostState, ret.PostRevision = post.State, post.Revision
	destination := target.EnrichmentTargetInput
	destination.CollectionRevision = collection.Revision
	ret.ReleasedTargetUUID, err = archive.EnrichmentTargetIdentity(destination)
	if err != nil {
		return nil, err
	}
	switch {
	case target.CollectionUUID != collection.UUID || target.CollectionRevision >= collection.Revision:
		ret.Disposition = "collection_changed"
	case collection.State != "active":
		ret.Disposition = "collection_disabled"
	case post.State != "active":
		ret.Disposition = "post_forgotten"
	case target.State != "pending" || target.Revision > int(^uint(0)>>1)-2:
		ret.Disposition = "target_changed"
	default:
		// A previous attempt may retain staging/checkpoints even if this target's
		// current revision is unbound. Such work needs explicit checkpoint review.
		var attempted, replacement bool
		if err := dbWrapper.Get(ctx, &attempted, "SELECT EXISTS(SELECT 1 FROM enrichment_job_targets WHERE target_uuid=?)", target.UUID); err != nil {
			return nil, err
		}
		if err := dbWrapper.Get(ctx, &replacement, "SELECT EXISTS(SELECT 1 FROM enrichment_targets WHERE uuid=?)", ret.ReleasedTargetUUID); err != nil {
			return nil, err
		}
		switch {
		case attempted:
			ret.Disposition = "worker_history"
		case replacement:
			ret.Disposition = "replacement_exists"
		default:
			ret.Disposition = "eligible"
		}
	}
	return ret, nil
}

func (s *EnrichmentWorkStore) RebindCandidates(ctx context.Context, collectionID string, revision int, after *models.EnrichmentRebindCursor, limit int) ([]models.EnrichmentRebindCandidate, error) {
	if !validSourceRunUUID(collectionID) || revision < 1 || limit < 1 || limit > archive.MaxEnrichmentActivationTargets {
		return nil, models.ErrEnrichmentInvalid
	}
	cursor := models.EnrichmentRebindCursor{}
	if after != nil {
		cursor = *after
		if cursor.CollectionRevision < 1 || cursor.CollectionRevision >= revision || !validSourceRunUUID(cursor.TargetUUID) {
			return nil, models.ErrEnrichmentInvalid
		}
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	if collection == nil || collection.Revision != revision {
		return nil, models.ErrEnrichmentConflict
	}
	var targets []models.EnrichmentTarget
	err = dbWrapper.Select(ctx, &targets, `SELECT t.*,u.url FROM enrichment_targets t INDEXED BY enrichment_targets_pending_scope
JOIN source_post_urls u ON u.uuid=t.url_uuid
WHERE t.collection_uuid=? AND t.state='pending' AND t.collection_revision<?
AND (t.collection_revision,t.uuid)>(?,?) ORDER BY t.collection_revision,t.uuid LIMIT ?`, collectionID, revision, cursor.CollectionRevision, cursor.TargetUUID, limit)
	if err != nil {
		return nil, err
	}
	ret := make([]models.EnrichmentRebindCandidate, 0, len(targets))
	for _, target := range targets {
		if err := validateEnrichmentTarget(&target); err != nil {
			return nil, err
		}
		row, err := s.rebindCandidate(ctx, &target, collection)
		if err != nil {
			return nil, err
		}
		ret = append(ret, *row)
	}
	return ret, nil
}

func (s *EnrichmentWorkStore) PreviewRebind(ctx context.Context, input models.EnrichmentRebindInput) (*models.EnrichmentRebindPlan, error) {
	input, _, err := archive.PrepareEnrichmentRebind(input)
	if err != nil {
		return nil, err
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, input.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if collection == nil || collection.State != "active" || collection.Revision != input.CollectionRevision {
		return nil, models.ErrEnrichmentConflict
	}
	ret := &models.EnrichmentRebindPlan{Version: 1, Input: input, Collection: *collection,
		Activation: models.EnrichmentActivationPlan{Version: 1, Input: models.EnrichmentActivationInput{UUID: archive.EnrichmentRebindActivationUUID(input.UUID)}}}
	var used bool
	if err := dbWrapper.Get(ctx, &used, "SELECT EXISTS(SELECT 1 FROM enrichment_activations WHERE uuid=?)", ret.Activation.Input.UUID); err != nil {
		return nil, err
	}
	if used {
		return nil, models.ErrEnrichmentConflict
	}
	destinations := map[string]bool{}
	for _, ref := range input.Targets {
		target, err := s.Target(ctx, ref.TargetUUID)
		if err != nil {
			return nil, err
		}
		if target == nil || target.Revision != ref.Revision {
			return nil, models.ErrEnrichmentConflict
		}
		candidate, err := s.rebindCandidate(ctx, target, collection)
		if err != nil {
			return nil, err
		}
		if candidate.Disposition != "eligible" || destinations[candidate.ReleasedTargetUUID] {
			return nil, models.ErrEnrichmentConflict
		}
		destinations[candidate.ReleasedTargetUUID] = true
		held := models.EnrichmentTargetRef{TargetUUID: ref.TargetUUID, Revision: ref.Revision + 1}
		ret.Activation.Input.Targets = append(ret.Activation.Input.Targets, models.EnrichmentActivationSelection{EnrichmentTargetRef: held, CollectionRevision: collection.Revision})
		ret.Activation.Entries = append(ret.Activation.Entries, models.EnrichmentActivationEntry{EnrichmentTargetRef: held,
			ReleasedTargetUUID: candidate.ReleasedTargetUUID, ReleasedRevision: 1, ActivationCollectionRevision: collection.Revision,
			PostUUID: target.PostUUID, PostRevision: candidate.PostRevision, URLUUID: target.URLUUID, URL: target.URL,
			CollectionUUID: target.CollectionUUID, CollectionRevision: target.CollectionRevision, Policy: target.Policy, Priority: target.Priority, NotBefore: target.NotBefore})
	}
	ret.Activation.PlanSHA256, err = archive.EnrichmentActivationDigest(ret.Activation)
	if err != nil {
		return nil, err
	}
	ret.PlanSHA256, err = archive.EnrichmentRebindDigest(*ret)
	return ret, err
}

func (s *EnrichmentWorkStore) Rebinding(ctx context.Context, id string) (*models.EnrichmentRebinding, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrEnrichmentInvalid
	}
	var row enrichmentRebindingRow
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM enrichment_rebindings WHERE uuid=?", id)
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
	activation, err := s.Activation(ctx, row.ActivationUUID)
	if err != nil {
		return nil, err
	}
	if activation == nil || !reflect.DeepEqual(activation.EnrichmentActivationPlan, plan.Activation) || !activation.CreatedAt.Equal(row.CreatedAt) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var targets []struct {
		UUID       string `db:"target_uuid"`
		Previous   int    `db:"previous_revision"`
		Held       int    `db:"held_revision"`
		Activation string `db:"activation_uuid"`
	}
	if err := dbWrapper.Select(ctx, &targets, "SELECT target_uuid,previous_revision,held_revision,activation_uuid FROM enrichment_rebinding_targets WHERE rebinding_uuid=? ORDER BY target_uuid LIMIT 101", id); err != nil {
		return nil, err
	}
	if len(targets) != len(plan.Input.Targets) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	for i, target := range targets {
		ref := plan.Input.Targets[i]
		if target.UUID != ref.TargetUUID || target.Previous != ref.Revision || target.Held != ref.Revision+1 || target.Activation != row.ActivationUUID {
			return nil, models.ErrSourcePayloadCorrupt
		}
	}
	return &models.EnrichmentRebinding{EnrichmentRebindPlan: *plan, CreatedAt: row.CreatedAt}, nil
}

func (s *EnrichmentWorkStore) Rebind(ctx context.Context, input models.EnrichmentRebindInput, expected string, now time.Time) (*models.EnrichmentRebinding, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	input, inputSHA, err := archive.PrepareEnrichmentRebind(input)
	if err != nil || !archive.ValidSHA256(expected) || !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	prior, err := s.Rebinding(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		_, priorSHA, err := archive.PrepareEnrichmentRebind(prior.Input)
		if err != nil || priorSHA != inputSHA || prior.PlanSHA256 != expected {
			return nil, models.ErrEnrichmentConflict
		}
		return prior, nil
	}
	plan, err := s.PreviewRebind(ctx, input)
	if err != nil {
		return nil, err
	}
	if plan.PlanSHA256 != expected {
		return nil, models.ErrEnrichmentConflict
	}
	complete := enrichmentAtomic(ctx)
	for i, ref := range plan.Input.Targets {
		entry := plan.Activation.Entries[i]
		held, err := s.Schedule(ctx, ref.TargetUUID, ref.Revision, models.EnrichmentSchedule{State: "held", Priority: entry.Priority, NotBefore: entry.NotBefore, Reason: "collection_rebind"}, now)
		if err != nil {
			return nil, err
		}
		if held.Revision != ref.Revision+1 {
			return nil, models.ErrEnrichmentConflict
		}
	}
	activation, err := s.Activate(ctx, plan.Activation.Input, plan.Activation.PlanSHA256, now)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(plan.Activation, activation.EnrichmentActivationPlan) {
		return nil, models.ErrEnrichmentConflict
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO enrichment_rebindings(uuid,input_sha256,plan_sha256,activation_uuid,plan,created_at) VALUES(?,?,?,?,?,?)", input.UUID, inputSHA, expected, activation.Input.UUID, string(body), now.UTC()); err != nil {
		return nil, err
	}
	for _, ref := range input.Targets {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO enrichment_rebinding_targets VALUES(?,?,?,?,?)", input.UUID, activation.Input.UUID, ref.TargetUUID, ref.Revision, ref.Revision+1); err != nil {
			return nil, err
		}
	}
	ret, err := s.Rebinding(ctx, input.UUID)
	*complete = err == nil
	return ret, err
}
