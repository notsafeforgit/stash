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

func activationSnapshot(ctx context.Context, snapshot, expected string) error {
	if snapshot == "" {
		return nil
	}
	progress, err := (&AutomationTranslationImportStore{}).Find(ctx, snapshot)
	if err != nil {
		return err
	}
	if progress == nil || progress.State == "running" || progress.ManifestSHA256 != expected {
		return models.ErrTranslationWorkConflict
	}
	return nil
}

func activationSourceTarget(ctx context.Context, input models.TranslationActivationInput, ref models.TranslationTargetRef) error {
	if input.SnapshotUUID == "" {
		return nil
	}
	var found bool
	err := dbWrapper.Get(ctx, &found, `SELECT EXISTS(SELECT 1 FROM automation_translation_records
 WHERE snapshot_uuid=? AND target_uuid=? AND target_revision=? AND disposition='held')`, input.SnapshotUUID, ref.TargetUUID, ref.Revision)
	if err != nil {
		return err
	}
	if !found {
		return models.ErrTranslationWorkConflict
	}
	return nil
}

func (s *TranslationWorkStore) PreviewActivation(ctx context.Context, input models.TranslationActivationInput) (*models.TranslationActivationPlan, error) {
	input, _, err := archive.PrepareTranslationActivation(input)
	if err != nil {
		return nil, err
	}
	if err := activationSnapshot(ctx, input.SnapshotUUID, input.ManifestSHA256); err != nil {
		return nil, err
	}
	ret := &models.TranslationActivationPlan{Version: 1, Input: input}
	for _, ref := range input.Targets {
		if err := activationSourceTarget(ctx, input, ref); err != nil {
			return nil, err
		}
		target, err := s.Target(ctx, ref.TargetUUID)
		if err != nil {
			return nil, err
		}
		if target == nil || target.Revision != ref.Revision || target.State != "held" {
			return nil, models.ErrTranslationWorkConflict
		}
		if _, err := activePostLink(ctx, target.PostUUID); err != nil {
			return nil, err
		}
		ret.Entries = append(ret.Entries, models.TranslationActivationEntry{TranslationTargetRef: ref,
			RequestUUID: target.RequestUUID, PostUUID: target.PostUUID, CollectionUUID: target.CollectionUUID, CollectionRevision: target.CollectionRevision,
			Field: target.Field, Priority: target.Priority, NotBefore: target.NotBefore})
	}
	ret.PlanSHA256, err = archive.TranslationActivationDigest(*ret)
	return ret, err
}

func (s *TranslationWorkStore) Activation(ctx context.Context, id string) (*models.TranslationActivation, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrTranslationWorkInvalid
	}
	var row translationActivationRecord
	err := dbWrapper.Get(ctx, &row, "SELECT * FROM translation_activations WHERE uuid=?", id)
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
	ret := &models.TranslationActivation{TranslationActivationPlan: *plan, CreatedAt: row.CreatedAt}
	err = dbWrapper.Select(ctx, &ret.Activated, `SELECT target_uuid,activated_revision AS target_revision FROM translation_activation_targets WHERE activation_uuid=? ORDER BY target_uuid`, id)
	if err != nil {
		return nil, err
	}
	if len(ret.Activated) != len(plan.Input.Targets) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	for i, ref := range ret.Activated {
		prior := plan.Input.Targets[i]
		if ref.TargetUUID != prior.TargetUUID || ref.Revision != prior.Revision+1 {
			return nil, models.ErrSourcePayloadCorrupt
		}
	}
	return ret, nil
}

func (s *TranslationWorkStore) Activate(ctx context.Context, input models.TranslationActivationInput, expected string, now time.Time) (*models.TranslationActivation, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	input, inputSHA, err := archive.PrepareTranslationActivation(input)
	if err != nil || !archive.ValidSHA256(expected) || !validJobTime(now) {
		return nil, models.ErrTranslationWorkInvalid
	}
	prior, err := s.Activation(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		_, priorSHA, err := archive.PrepareTranslationActivation(prior.Input)
		if err != nil || priorSHA != inputSHA || prior.PlanSHA256 != expected {
			return nil, models.ErrTranslationWorkConflict
		}
		return prior, nil
	}
	plan, err := s.PreviewActivation(ctx, input)
	if err != nil {
		return nil, err
	}
	if plan.PlanSHA256 != expected {
		return nil, models.ErrTranslationWorkConflict
	}
	complete := translationWorkAtomic(ctx)
	for _, entry := range plan.Entries {
		if _, err := s.ScheduleTarget(ctx, entry.TargetUUID, entry.Revision,
			models.TranslationTargetSchedule{State: "pending", Priority: entry.Priority, NotBefore: entry.NotBefore}, now); err != nil {
			return nil, err
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
	_, err = dbWrapper.Exec(ctx, `INSERT INTO translation_activations(uuid,input_sha256,plan_sha256,snapshot_uuid,manifest_sha256,plan,created_at) VALUES(?,?,?,?,?,?,?)`,
		input.UUID, inputSHA, expected, snapshot, manifest, string(body), now.UTC())
	if err != nil {
		return nil, err
	}
	for _, ref := range input.Targets {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO translation_activation_targets(activation_uuid,target_uuid,previous_revision,activated_revision) VALUES(?,?,?,?)`,
			input.UUID, ref.TargetUUID, ref.Revision, ref.Revision+1); err != nil {
			return nil, err
		}
	}
	ret, err := s.Activation(ctx, input.UUID)
	*complete = err == nil
	return ret, err
}

func (s *AutomationTranslationImportStore) HeldTargets(ctx context.Context, id, expected string, after int64, limit int) ([]models.AutomationTranslationCandidate, error) {
	if !validSourceRunUUID(id) || !archive.ValidSHA256(expected) || after < 0 || limit < 1 || limit > archive.MaxTranslationActivationTargets {
		return nil, models.ErrTranslationWorkInvalid
	}
	if err := activationSnapshot(ctx, id, expected); err != nil {
		return nil, err
	}
	ret := []models.AutomationTranslationCandidate{}
	err := dbWrapper.Select(ctx, &ret, `SELECT r.ordinal,r.target_uuid,r.target_revision,t.revision AS current_revision,t.state,p.state AS post_state,
 t.request_uuid,t.post_uuid,t.collection_uuid,t.collection_revision,t.field,t.priority,t.not_before FROM automation_translation_records r INDEXED BY automation_translation_held
 JOIN translation_targets t ON t.uuid=r.target_uuid JOIN source_posts p ON p.uuid=t.post_uuid
 WHERE r.snapshot_uuid=? AND r.ordinal>? AND r.disposition='held' ORDER BY r.ordinal LIMIT ?`, id, after, limit)
	if err != nil {
		return nil, err
	}
	for i := range ret {
		row := &ret[i]
		switch {
		case row.PostState != "active":
			row.Disposition = "post_forgotten"
		case row.State == "completed":
			row.Disposition = "completed"
		case row.State != "held" || row.CurrentRevision != row.Revision:
			row.Disposition = "changed"
		default:
			row.Disposition = "eligible"
		}
	}
	return ret, nil
}
