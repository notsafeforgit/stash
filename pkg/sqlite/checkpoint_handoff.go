package sqlite

import (
	"bytes"
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

func checkpointHandoffInput(input models.CheckpointHandoffInput) (string, error) {
	if !validSourceRunUUID(input.UUID) || !validSourceRunUUID(input.EvidenceUUID) || !archive.ValidSHA256(input.EvidencePlanSHA256) ||
		input.TargetRevision < 1 || input.CollectionRevision < 1 || !archive.ValidSHA256(input.PolicySHA256) ||
		input.ExtractorVersion == "" || len(input.ExtractorVersion) > 128 || strings.ContainsAny(input.ExtractorVersion, "\r\n\x00") {
		return "", models.ErrEnrichmentInvalid
	}
	return sourceSignature("stash-checkpoint-handoff-request-v1", input)
}

func checkpointHandoffDigest(raw []byte) (string, error) {
	value, err := archive.DecodeJSONObject(raw, 1<<20)
	if err != nil {
		return "", err
	}
	delete(value, "plan_sha256")
	return sourceSignature("stash-checkpoint-handoff-plan-v1", value)
}

// Reconstruct rather than storing another full copy of the metadata. Verify
// retained identities against accepted native captures before exposing a seed.
func checkpointEvidenceSeed(get enrichmentGet, selectRows enrichmentSelect, accepted *models.CheckpointEvidenceAcceptance, extractor string) (json.RawMessage, error) {
	var row catalogEvidenceRow
	if err := get(&row, "SELECT ordinal,data,data_sha256 FROM automation_snapshot_records WHERE snapshot_uuid=? AND ordinal=?",
		accepted.Input.SnapshotUUID, accepted.Input.Ordinal); err != nil {
		return nil, err
	}
	expected, body, err := prepareAutomationCheckpoint(row)
	if err != nil || expected == nil || expected.Outcome != "mapped" || expected.SourceSHA256 != accepted.SourceSHA256 ||
		*expected.StagedSHA256 != accepted.StagedSHA256 || *expected.BodySHA256 != accepted.BodySHA256 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	staging, err := scrape.DecodeStaging(body)
	if err != nil {
		return nil, err
	}
	var identified bool
	if err := get(&identified, "SELECT EXISTS(SELECT 1 FROM source_post_identifiers WHERE post_uuid=? AND namespace=? AND value=?)",
		accepted.Target.PostUUID, accepted.PostNamespace, accepted.PostValue); err != nil {
		return nil, err
	}
	if !identified {
		return nil, models.ErrSourcePayloadCorrupt
	}
	seen := map[string]bool{}
	for _, binding := range accepted.Captures {
		if seen[binding.CaptureUUID] {
			continue
		}
		seen[binding.CaptureUUID] = true
		capture, err := findSourceCapture(get, selectRows, binding.CaptureUUID)
		if err != nil {
			return nil, err
		}
		if capture == nil || capture.PostUUID != accepted.Target.PostUUID || capture.Origin != "legacy-enrichment" ||
			capture.RetentionPolicy != "legacy-retained-v1" || !capture.CapturedAt.IsZero() || capture.RecordedAt == nil || !capture.RecordedAt.Equal(accepted.CreatedAt) ||
			!reflect.DeepEqual(capture.ExtractorVersion, staging.ExtractorVersion) || len(capture.Contexts) != 0 {
			return nil, models.ErrSourcePayloadCorrupt
		}
		raw, err := archive.RestoreCapture(capture.Payload)
		if err != nil || sourceDigest(raw) != binding.PayloadSHA {
			return nil, models.ErrSourcePayloadCorrupt
		}
		metadata, err := archive.CapturedMetadata(raw)
		if err != nil || !reflect.DeepEqual(capture.Metadata, metadata) || capture.Platform != archive.CapturedPostPlatform(models.SourcePostIdentifier{Namespace: accepted.PostNamespace, Value: accepted.PostValue}) {
			return nil, models.ErrSourcePayloadCorrupt
		}
	}
	return scrape.PrepareLegacyEnrichmentSeed(accepted, body, extractor)
}

func (s *AutomationCheckpointImportStore) PreviewHandoff(ctx context.Context, input models.CheckpointHandoffInput) (*models.CheckpointHandoffPlan, error) {
	if _, err := checkpointHandoffInput(input); err != nil {
		return nil, err
	}
	accepted, err := s.EvidenceAcceptance(ctx, input.EvidenceUUID)
	if err != nil {
		return nil, err
	}
	if accepted == nil || accepted.PlanSHA256 != input.EvidencePlanSHA256 || accepted.Target.Revision != input.TargetRevision {
		return nil, models.ErrEnrichmentConflict
	}
	target, err := (&EnrichmentWorkStore{}).Target(ctx, accepted.Target.UUID)
	if err != nil {
		return nil, err
	}
	if target == nil || !reflect.DeepEqual(target, &accepted.Target) {
		return nil, models.ErrEnrichmentConflict
	}
	post, err := activePostLink(ctx, target.PostUUID)
	if err != nil {
		return nil, err
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, target.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if collection == nil || collection.Revision != input.CollectionRevision || collection.State != "active" ||
		(collection.Namespace != "" && collection.Namespace != accepted.PostNamespace) {
		return nil, models.ErrEnrichmentConflict
	}
	if collection.RootUUID != nil {
		root, err := (&MediaRootStore{}).Find(ctx, *collection.RootUUID)
		if err != nil {
			return nil, err
		}
		if root == nil || root.State != "active" {
			return nil, models.ErrEnrichmentConflict
		}
	}
	history, err := (&SourceCollectionStore{}).History(ctx, collection.UUID, collection.Revision-1, 1)
	if err != nil {
		return nil, err
	}
	if len(history) != 1 || history[0].Revision != collection.Revision {
		return nil, models.ErrSourcePayloadCorrupt
	}
	seed, err := checkpointEvidenceSeed(func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) },
		func(out any, query string, args ...any) error { return dbWrapper.Select(ctx, out, query, args...) }, accepted, input.ExtractorVersion)
	if err != nil {
		return nil, err
	}
	parsed, err := archive.ParseEnrichmentTranscript(seed)
	if err != nil {
		return nil, err
	}
	entry := models.EnrichmentActivationEntry{EnrichmentTargetRef: models.EnrichmentTargetRef{TargetUUID: target.UUID, Revision: target.Revision},
		PostUUID: target.PostUUID, URLUUID: target.URLUUID, CollectionUUID: collection.UUID, ActivationCollectionRevision: collection.Revision, Policy: target.Policy}
	if err := enrichmentActivationDestination(&entry); err != nil {
		return nil, err
	}
	if entry.ReleasedTargetUUID != target.UUID {
		prior, err := (&EnrichmentWorkStore{}).Target(ctx, entry.ReleasedTargetUUID)
		if err != nil {
			return nil, err
		}
		if prior != nil {
			return nil, models.ErrEnrichmentConflict
		}
	}
	plan := &models.CheckpointHandoffPlan{Version: 1, Input: input, Target: *target, PostRevision: post.Revision, Collection: history[0], EvidenceCreatedAt: accepted.CreatedAt,
		CapturePolicy: archive.CaptureContextPolicy, ReleasedTargetUUID: entry.ReleasedTargetUUID, ReleasedRevision: entry.ReleasedRevision,
		SeedSHA256: sourceDigest(seed), SeedBytes: len(seed), RetainedCount: len(parsed.Records), PendingCount: len(parsed.Pending), UnscopedCount: accepted.UnscopedCount}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	plan.PlanSHA256, err = checkpointHandoffDigest(body)
	return plan, err
}

type checkpointHandoffRow struct {
	UUID               string    `db:"uuid"`
	EvidenceUUID       string    `db:"evidence_uuid"`
	TargetUUID         string    `db:"target_uuid"`
	TargetRevision     int       `db:"target_revision"`
	CollectionUUID     string    `db:"collection_uuid"`
	CollectionRevision int       `db:"collection_revision"`
	InputSHA256        string    `db:"input_sha256"`
	PlanSHA256         string    `db:"plan_sha256"`
	SeedSHA256         string    `db:"seed_sha256"`
	Plan               string    `db:"plan"`
	CreatedAt          time.Time `db:"created_at"`
}

func (row checkpointHandoffRow) decode() (*models.CheckpointHandoff, error) {
	var plan models.CheckpointHandoffPlan
	d := json.NewDecoder(bytes.NewReader([]byte(row.Plan)))
	d.DisallowUnknownFields()
	digest, err := checkpointHandoffDigest([]byte(row.Plan))
	if err != nil || digest != row.PlanSHA256 || d.Decode(&plan) != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	inputSHA, err := checkpointHandoffInput(plan.Input)
	if err != nil || inputSHA != row.InputSHA256 || plan.Version != 1 || plan.Input.UUID != row.UUID || plan.PlanSHA256 != row.PlanSHA256 ||
		plan.Input.EvidenceUUID != row.EvidenceUUID || plan.Target.UUID != row.TargetUUID || plan.Target.Revision != row.TargetRevision ||
		plan.Input.TargetRevision != row.TargetRevision || plan.Collection.UUID != row.CollectionUUID || plan.Collection.Revision != row.CollectionRevision ||
		plan.Collection.UUID != plan.Target.CollectionUUID || plan.Input.CollectionRevision != row.CollectionRevision || plan.Collection.State != "active" ||
		plan.SeedSHA256 != row.SeedSHA256 || !archive.ValidSHA256(row.SeedSHA256) || plan.CapturePolicy != archive.CaptureContextPolicy ||
		plan.RetainedCount < 1 || plan.RetainedCount > archive.MaxEnrichmentCaptures || plan.PendingCount < 0 || plan.PendingCount > archive.MaxEnrichmentReferences ||
		plan.PostRevision < 1 || plan.UnscopedCount < 0 || plan.SeedBytes < 1 || plan.SeedBytes > archive.MaxEnrichmentTranscriptBytes ||
		validateEnrichmentTarget(&plan.Target) != nil || plan.Target.State != "review" || plan.Target.Reason != "legacy_checkpoint_conversion" ||
		!validJobTime(row.CreatedAt) || !validJobTime(plan.EvidenceCreatedAt) || !validJobTime(plan.Collection.RecordedAt) ||
		row.CreatedAt.Before(plan.EvidenceCreatedAt) || row.CreatedAt.Before(plan.Target.UpdatedAt) || row.CreatedAt.Before(plan.Collection.RecordedAt) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	entry := models.EnrichmentActivationEntry{EnrichmentTargetRef: models.EnrichmentTargetRef{TargetUUID: plan.Target.UUID, Revision: plan.Target.Revision},
		PostUUID: plan.Target.PostUUID, URLUUID: plan.Target.URLUUID, CollectionUUID: plan.Collection.UUID, ActivationCollectionRevision: plan.Collection.Revision, Policy: plan.Target.Policy}
	if enrichmentActivationDestination(&entry) != nil || entry.ReleasedTargetUUID != plan.ReleasedTargetUUID || entry.ReleasedRevision != plan.ReleasedRevision {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.CheckpointHandoff{CheckpointHandoffPlan: plan, CreatedAt: row.CreatedAt.UTC()}, nil
}

func readCheckpointHandoff(get enrichmentGet, selectRows enrichmentSelect, id string) (*models.CheckpointHandoff, json.RawMessage, error) {
	if !validSourceRunUUID(id) {
		return nil, nil, models.ErrEnrichmentInvalid
	}
	var row checkpointHandoffRow
	err := get(&row, "SELECT * FROM checkpoint_handoffs WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	ret, err := row.decode()
	if err != nil {
		return nil, nil, err
	}
	accepted, err := readCheckpointEvidence(get, selectRows, ret.Input.EvidenceUUID)
	if err != nil {
		return nil, nil, err
	}
	if accepted == nil || accepted.PlanSHA256 != ret.Input.EvidencePlanSHA256 || !accepted.CreatedAt.Equal(ret.EvidenceCreatedAt) ||
		!reflect.DeepEqual(accepted.Target, ret.Target) || ret.PostRevision < accepted.PostRevision || ret.UnscopedCount != accepted.UnscopedCount ||
		(ret.Collection.Namespace != "" && ret.Collection.Namespace != accepted.PostNamespace) {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	var collection sourceCollectionRow
	if err := get(&collection, sourceCollectionSelect+" WHERE b.uuid=? AND r.revision=?", ret.Collection.UUID, ret.Collection.Revision); err != nil {
		return nil, nil, err
	}
	history := models.SourceCollectionRevision{SourceCollection: *collection.resolve(), Origin: collection.Origin, Reason: collection.Reason, RecordedAt: collection.RecordedAt.Timestamp}
	var revision int
	if err := get(&revision, "SELECT revision FROM source_posts WHERE uuid=?", ret.Target.PostUUID); err != nil {
		return nil, nil, err
	}
	if !reflect.DeepEqual(history, ret.Collection) || revision < ret.PostRevision {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	seed, err := checkpointEvidenceSeed(get, selectRows, accepted, ret.Input.ExtractorVersion)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := archive.ParseEnrichmentTranscript(seed)
	if err != nil || len(seed) != ret.SeedBytes || sourceDigest(seed) != ret.SeedSHA256 || len(parsed.Records) != ret.RetainedCount || len(parsed.Pending) != ret.PendingCount {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	return ret, seed, nil
}

func (s *AutomationCheckpointImportStore) Handoff(ctx context.Context, id string) (*models.CheckpointHandoff, error) {
	ret, _, err := readCheckpointHandoff(func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) },
		func(out any, query string, args ...any) error { return dbWrapper.Select(ctx, out, query, args...) }, id)
	return ret, err
}

func (s *AutomationCheckpointImportStore) HandoffSeed(ctx context.Context, id string) (*models.CheckpointHandoffSeed, error) {
	ret, body, err := readCheckpointHandoff(func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) },
		func(out any, query string, args ...any) error { return dbWrapper.Select(ctx, out, query, args...) }, id)
	if err != nil || ret == nil {
		return nil, err
	}
	return &models.CheckpointHandoffSeed{HandoffUUID: id, PlanSHA256: ret.PlanSHA256, SHA256: ret.SeedSHA256, Body: body}, nil
}

func (s *AutomationCheckpointImportStore) AcceptHandoff(ctx context.Context, input models.CheckpointHandoffInput, expected string, now time.Time) (*models.CheckpointHandoff, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	inputSHA, err := checkpointHandoffInput(input)
	if err != nil || !archive.ValidSHA256(expected) || !validJobTime(now) {
		return nil, models.ErrEnrichmentInvalid
	}
	prior, err := s.Handoff(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.Input != input || prior.PlanSHA256 != expected {
			return nil, models.ErrEnrichmentConflict
		}
		return prior, nil
	}
	plan, err := s.PreviewHandoff(ctx, input)
	if err != nil {
		return nil, err
	}
	if plan.PlanSHA256 != expected || now.Before(plan.EvidenceCreatedAt) || now.Before(plan.Target.UpdatedAt) || now.Before(plan.Collection.RecordedAt) {
		return nil, models.ErrEnrichmentConflict
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	complete := enrichmentAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, `INSERT INTO checkpoint_handoffs(uuid,evidence_uuid,target_uuid,target_revision,collection_uuid,collection_revision,input_sha256,plan_sha256,seed_sha256,plan,created_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?)`, input.UUID, input.EvidenceUUID, plan.Target.UUID, input.TargetRevision, plan.Collection.UUID, input.CollectionRevision, inputSHA, expected, plan.SeedSHA256, string(body), now.UTC())
	if err != nil {
		return nil, err
	}
	ret, err := s.Handoff(ctx, input.UUID)
	*complete = err == nil
	return ret, err
}
