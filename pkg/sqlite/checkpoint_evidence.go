package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func checkpointEvidenceInput(input models.CheckpointEvidenceInput) (string, error) {
	if !validSourceRunUUID(input.UUID) || !validSourceRunUUID(input.SnapshotUUID) || !archive.ValidSHA256(input.ManifestSHA256) ||
		input.Ordinal < 1 || input.TargetRevision < 1 {
		return "", models.ErrEnrichmentInvalid
	}
	return sourceSignature("stash-checkpoint-evidence-request-v1", input)
}

func checkpointEvidenceDigest(raw []byte) (string, error) {
	value, err := archive.DecodeJSONObject(raw, 1<<20)
	if err != nil {
		return "", err
	}
	delete(value, "plan_sha256")
	return sourceSignature("stash-checkpoint-evidence-plan-v1", value)
}

func (s *AutomationCheckpointImportStore) PreviewEvidence(ctx context.Context, input models.CheckpointEvidenceInput) (*models.CheckpointEvidencePlan, error) {
	if _, err := checkpointEvidenceInput(input); err != nil {
		return nil, err
	}
	var accepted bool
	if err := dbWrapper.Get(ctx, &accepted, "SELECT EXISTS(SELECT 1 FROM checkpoint_evidence_acceptances WHERE snapshot_uuid=? AND ordinal=?)", input.SnapshotUUID, input.Ordinal); err != nil {
		return nil, err
	}
	if accepted {
		return nil, models.ErrEnrichmentConflict
	}
	progress, err := s.Find(ctx, input.SnapshotUUID)
	if err != nil {
		return nil, err
	}
	if progress == nil || progress.State == "running" || progress.ManifestSHA256 != input.ManifestSHA256 {
		return nil, models.ErrEnrichmentConflict
	}
	detail, err := s.Record(ctx, input.SnapshotUUID, input.Ordinal)
	if err != nil {
		return nil, err
	}
	source, err := (&AutomationEnrichmentImportStore{}).Record(ctx, input.SnapshotUUID, input.Ordinal)
	if err != nil {
		return nil, err
	}
	if detail == nil || detail.Outcome != "mapped" || source == nil || source.Disposition != "staged_review" ||
		source.TargetUUID == nil || source.TargetRevision == nil || *source.TargetRevision != input.TargetRevision {
		return nil, models.ErrEnrichmentConflict
	}
	target, err := (&EnrichmentWorkStore{}).Target(ctx, *source.TargetUUID)
	if err != nil {
		return nil, err
	}
	if target == nil || target.Revision != input.TargetRevision || target.State != "review" || target.Reason != "legacy_checkpoint_conversion" {
		return nil, models.ErrEnrichmentConflict
	}
	post, err := activePostLink(ctx, target.PostUUID)
	if err != nil {
		return nil, err
	}
	staging, err := scrape.DecodeStaging(detail.Body)
	if err != nil {
		return nil, err
	}
	// The preview has no recording time. Its fixed placeholder is never persisted
	// or included in the plan; acceptance assigns the real archive receipt time.
	captures, err := scrape.PrepareStagedCaptures(input.SnapshotUUID, input.Ordinal, target.PostUUID, staging, time.Unix(1, 0).UTC())
	if err != nil {
		return nil, err
	}
	plan := &models.CheckpointEvidencePlan{Version: 1, Input: input, SourceSHA256: detail.SourceSHA256,
		StagedSHA256: *detail.StagedSHA256, BodySHA256: *detail.BodySHA256, Target: *target, PostRevision: post.Revision,
		Captures: []models.CheckpointEvidenceCapture{}, RecordCount: detail.RecordCount, PendingCount: detail.PendingCount, UnscopedCount: detail.UnresolvedCount}
	for index, capture := range captures {
		raw, err := archive.RestoreCapture(&capture.Payload)
		if err != nil {
			return nil, err
		}
		reference, err := archive.ExtractCapturedPost(raw)
		if err != nil || reference == nil {
			return nil, models.ErrEnrichmentInvalid
		}
		if index == 0 {
			plan.PostNamespace, plan.PostValue = reference.Namespace, reference.Value
		} else if reference.Namespace != plan.PostNamespace || reference.Value != plan.PostValue {
			return nil, models.ErrEnrichmentConflict
		}
		plan.Captures = append(plan.Captures, models.CheckpointEvidenceCapture{BodyIndex: index, CaptureUUID: capture.UUID, PayloadSHA: sourceDigest(raw)})
	}
	store := &SourceEvidenceStore{}
	matched, err := store.FindPostByIdentifier(ctx, models.SourcePostIdentifier{Namespace: plan.PostNamespace, Value: plan.PostValue})
	if err != nil {
		return nil, err
	}
	if matched != nil && matched.UUID != target.PostUUID {
		return nil, models.ErrEnrichmentConflict
	}
	if matched == nil {
		// An imported post may still have only its catalog-local identity. The
		// reviewed source evidence may establish one unclaimed service identity;
		// it cannot replace a different service ID or silently merge two posts.
		identifiers, err := store.PostIdentifiers(ctx, target.PostUUID, nil, 100)
		if err != nil {
			return nil, err
		}
		if len(identifiers) == 100 {
			return nil, models.ErrEnrichmentConflict
		}
		for _, reference := range identifiers {
			if !strings.HasPrefix(reference.Namespace, "legacy:catalog:") {
				return nil, models.ErrEnrichmentConflict
			}
		}
		plan.AssignPostID = true
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	plan.PlanSHA256, err = checkpointEvidenceDigest(body)
	return plan, err
}

type checkpointEvidenceRow struct {
	UUID           string    `db:"uuid"`
	SnapshotUUID   string    `db:"snapshot_uuid"`
	Ordinal        int64     `db:"ordinal"`
	TargetUUID     string    `db:"target_uuid"`
	TargetRevision int       `db:"target_revision"`
	InputSHA256    string    `db:"input_sha256"`
	PlanSHA256     string    `db:"plan_sha256"`
	Plan           string    `db:"plan"`
	CreatedAt      time.Time `db:"created_at"`
}

func (row checkpointEvidenceRow) decode() (*models.CheckpointEvidenceAcceptance, error) {
	var plan models.CheckpointEvidencePlan
	digest, err := checkpointEvidenceDigest([]byte(row.Plan))
	if err != nil || digest != row.PlanSHA256 || json.Unmarshal([]byte(row.Plan), &plan) != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	inputSHA, err := checkpointEvidenceInput(plan.Input)
	if err != nil || plan.Version != 1 || plan.Input.UUID != row.UUID || plan.Input.SnapshotUUID != row.SnapshotUUID ||
		plan.Input.Ordinal != row.Ordinal || inputSHA != row.InputSHA256 || plan.PlanSHA256 != row.PlanSHA256 ||
		plan.Target.UUID != row.TargetUUID || plan.Target.Revision != row.TargetRevision || plan.Input.TargetRevision != row.TargetRevision ||
		!validJobTime(row.CreatedAt) || len(plan.Captures) == 0 || len(plan.Captures) > 4096 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.CheckpointEvidenceAcceptance{CheckpointEvidencePlan: plan, CreatedAt: row.CreatedAt.UTC()}, nil
}

func (s *AutomationCheckpointImportStore) EvidenceAcceptance(ctx context.Context, id string) (*models.CheckpointEvidenceAcceptance, error) {
	return readCheckpointEvidence(func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) },
		func(out any, query string, args ...any) error { return dbWrapper.Select(ctx, out, query, args...) }, id)
}

func readCheckpointEvidence(get enrichmentGet, selectRows enrichmentSelect, id string) (*models.CheckpointEvidenceAcceptance, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrEnrichmentInvalid
	}
	var row checkpointEvidenceRow
	err := get(&row, "SELECT * FROM checkpoint_evidence_acceptances WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ret, err := row.decode()
	if err != nil {
		return nil, err
	}
	var bindings []models.CheckpointEvidenceCapture
	if err := selectRows(&bindings, `SELECT body_index,capture_uuid,payload_sha256 FROM checkpoint_evidence_captures
 WHERE acceptance_uuid=? ORDER BY body_index`, id); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(bindings, ret.Captures) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return ret, nil
}

func (s *AutomationCheckpointImportStore) AcceptEvidence(ctx context.Context, input models.CheckpointEvidenceInput, expected string, now time.Time) (*models.CheckpointEvidenceAcceptance, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	inputSHA, err := checkpointEvidenceInput(input)
	if err != nil || !archive.ValidSHA256(expected) || !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	previous, err := s.EvidenceAcceptance(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if previous != nil {
		if previous.Input != input || previous.PlanSHA256 != expected {
			return nil, models.ErrEnrichmentConflict
		}
		return previous, nil
	}
	plan, err := s.PreviewEvidence(ctx, input)
	if err != nil {
		return nil, err
	}
	if plan.PlanSHA256 != expected || now.Before(plan.Target.UpdatedAt) {
		return nil, models.ErrEnrichmentConflict
	}
	progress, err := s.Find(ctx, input.SnapshotUUID)
	if err != nil {
		return nil, err
	}
	updated, err := time.Parse(time.RFC3339Nano, progress.UpdatedAt)
	if err != nil || now.Before(updated) {
		return nil, models.ErrEnrichmentConflict
	}
	detail, err := s.Record(ctx, input.SnapshotUUID, input.Ordinal)
	if err != nil {
		return nil, err
	}
	staging, err := scrape.DecodeStaging(detail.Body)
	if err != nil {
		return nil, err
	}
	captures, err := scrape.PrepareStagedCaptures(input.SnapshotUUID, input.Ordinal, plan.Target.PostUUID, staging, now)
	if err != nil {
		return nil, err
	}
	complete := enrichmentAtomic(ctx)
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO checkpoint_evidence_acceptances(uuid,snapshot_uuid,ordinal,target_uuid,target_revision,input_sha256,plan_sha256,plan,created_at)
 VALUES(?,?,?,?,?,?,?,?,?)`, input.UUID, input.SnapshotUUID, input.Ordinal, plan.Target.UUID, plan.Target.Revision, inputSHA, expected, string(body), now.UTC())
	if err != nil {
		return nil, err
	}
	if plan.AssignPostID {
		if err := (&SourceEvidenceStore{}).AddPostIdentifier(ctx, plan.Target.PostUUID,
			models.SourcePostIdentifier{Namespace: plan.PostNamespace, Value: plan.PostValue}, plan.PostRevision); err != nil {
			return nil, err
		}
	}
	for index, capture := range captures {
		if _, err := (&SourceEvidenceStore{}).RecordCapture(ctx, capture); err != nil {
			return nil, err
		}
		if err := (&SourceCollectionStore{}).RecordCapture(ctx, models.CollectionCapture{CaptureUUID: capture.UUID,
			CollectionUUID: plan.Target.CollectionUUID, CollectionRevision: plan.Target.CollectionRevision}); err != nil {
			return nil, err
		}
		_, err := dbWrapper.Exec(ctx, "INSERT INTO checkpoint_evidence_captures VALUES(?,?,?,?)", input.UUID, index, capture.UUID, plan.Captures[index].PayloadSHA)
		if err != nil {
			return nil, err
		}
	}
	ret, err := s.EvidenceAcceptance(ctx, input.UUID)
	*complete = err == nil
	return ret, err
}
