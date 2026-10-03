package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeCheckpointHandoffSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeEnrichmentHandoffSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE checkpoint_handoffs; DELETE FROM native_migration_history WHERE version=1000064`)
	require.NoError(t, err)
}

func checkpointHandoffFixture(t *testing.T) (*automationSnapshotFixture, models.CheckpointHandoffInput) {
	t.Helper()
	f, evidence := checkpointEvidenceFixture(t)
	plan := checkpointEvidencePreview(t, f, evidence)
	accepted, err := acceptCheckpointEvidence(f, plan)
	require.NoError(t, err)
	input := models.CheckpointHandoffInput{UUID: uuid.NewString(), EvidenceUUID: accepted.Input.UUID, EvidencePlanSHA256: accepted.PlanSHA256,
		TargetRevision: accepted.Target.Revision, PolicySHA256: strings.Repeat("a", 64), ExtractorVersion: "1.32.15-dev"}
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		collection, err := f.repo.SourceCollection.Find(ctx, plan.Target.CollectionUUID)
		require.NoError(t, err)
		definition := collection.SourceCollectionDefinition
		definition.State = "active"
		revised, err := f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision,
			SourceCollectionDefinition: definition, Origin: "review", Reason: "Reviewed child retry destination"})
		require.NoError(t, err)
		input.CollectionRevision = revised.Revision
		return nil
	}))
	return f, input
}

func checkpointHandoffPreview(t *testing.T, f *automationSnapshotFixture, input models.CheckpointHandoffInput) *models.CheckpointHandoffPlan {
	t.Helper()
	var plan *models.CheckpointHandoffPlan
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		plan, err = f.repo.AutomationCheckpointImport.PreviewHandoff(ctx, input)
		return err
	}))
	return plan
}

func handoffTime(plan *models.CheckpointHandoffPlan) time.Time {
	stamp := plan.EvidenceCreatedAt
	if stamp.Before(plan.Collection.RecordedAt) {
		stamp = plan.Collection.RecordedAt
	}
	return stamp.Add(time.Hour)
}

func acceptCheckpointHandoff(f *automationSnapshotFixture, plan *models.CheckpointHandoffPlan) (*models.CheckpointHandoff, error) {
	var result *models.CheckpointHandoff
	err := f.repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.AutomationCheckpointImport.AcceptHandoff(ctx, plan.Input, plan.PlanSHA256, handoffTime(plan))
		return err
	})
	return result, err
}

func TestCheckpointHandoffPreservesEvidenceAndReviewWithoutInventingExecution(t *testing.T) {
	f, input := checkpointHandoffFixture(t)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"automation_snapshot_records", "automation_checkpoint_bodies", "checkpoint_evidence_acceptances", "checkpoint_evidence_captures",
		"enrichment_targets", "enrichment_target_history", "archive_jobs", "enrichment_job_attempts", "enrichment_checkpoints", "enrichment_checkpoint_receipts",
		"source_captures", "source_collection_captures", "capture_publisher_decisions", "source_account_identifiers"} {
		before[table] = albumJobRows(t, raw, table)
	}
	plan := checkpointHandoffPreview(t, f, input)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM checkpoint_handoffs"))
	require.Equal(t, 2, plan.RetainedCount)
	require.Equal(t, 1, plan.PendingCount)
	require.Equal(t, 1, plan.UnscopedCount)
	require.Equal(t, archive.CaptureContextPolicy, plan.CapturePolicy)
	require.NotEqual(t, plan.Target.UUID, plan.ReleasedTargetUUID, "active collection revision needs a distinct target")
	require.Equal(t, 1, plan.ReleasedRevision)
	receipt, err := acceptCheckpointHandoff(f, plan)
	require.NoError(t, err)
	require.Equal(t, *plan, receipt.CheckpointHandoffPlan)
	for table, values := range before {
		require.Equal(t, values, albumJobRows(t, raw, table), table)
	}
	var seed *models.CheckpointHandoffSeed
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		seed, err = f.repo.AutomationCheckpointImport.HandoffSeed(ctx, input.UUID)
		return err
	}))
	parsed, err := archive.ParseEnrichmentTranscript(seed.Body)
	require.NoError(t, err)
	require.Equal(t, archive.EnrichmentRetainedSchema, parsed.Schema)
	require.Equal(t, plan.SeedBytes, len(seed.Body))
	require.Equal(t, plan.SeedSHA256, seed.SHA256)
	require.Len(t, parsed.Records, plan.RetainedCount)
	require.Contains(t, string(seed.Body), "9007199254740993")
	require.Contains(t, string(seed.Body), `"observed_at":null`)
	require.Empty(t, parsed.Unresolved, "original unscoped references stay in accepted evidence")
	backup := filepath.Join(t.TempDir(), "handoff.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(backup))
	f.repo = f.db.Repository()
	// Later scheduling and collection decisions cannot erase a committed review.
	_, err = scheduleEnrichment(f.repo, &plan.Target, models.EnrichmentSchedule{State: "excluded", Priority: 5, Reason: "owner_excluded"}, handoffTime(plan).Add(time.Hour))
	require.NoError(t, err)
	definition := plan.Collection.SourceCollectionDefinition
	definition.State = "disabled"
	_, err = reviseEnrichmentCollection(f.repo, plan.Collection.UUID, plan.Collection.Revision, models.SourceCollectionInput{SourceCollectionDefinition: definition, Origin: "review"})
	require.NoError(t, err)
	replayed, err := acceptCheckpointHandoff(f, plan)
	require.NoError(t, err)
	require.Equal(t, receipt, replayed)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(backup))
	anonPath := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anon, err := sqlite.NewAnonymiser(f.db, anonPath)
	require.NoError(t, err)
	require.NoError(t, anon.Anonymise(t.Context()))
	anonymous := openRawDB(t, anonPath)
	defer anonymous.Close()
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM checkpoint_handoffs"))
}

func TestCheckpointHandoffRejectsUnreviewedOrChangedScope(t *testing.T) {
	f, input := checkpointHandoffFixture(t)
	plan := checkpointHandoffPreview(t, f, input)
	for _, change := range []func(*models.CheckpointHandoffInput){
		func(v *models.CheckpointHandoffInput) { v.EvidenceUUID = uuid.NewString() },
		func(v *models.CheckpointHandoffInput) { v.EvidencePlanSHA256 = strings.Repeat("0", 64) },
		func(v *models.CheckpointHandoffInput) { v.TargetRevision++ },
		func(v *models.CheckpointHandoffInput) { v.CollectionRevision++ },
	} {
		changed := input
		change(&changed)
		err := f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.AutomationCheckpointImport.PreviewHandoff(ctx, changed)
			return err
		})
		require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	}
	for _, change := range []func(*models.CheckpointHandoffInput){
		func(v *models.CheckpointHandoffInput) { v.PolicySHA256 = strings.Repeat("b", 64) },
		func(v *models.CheckpointHandoffInput) { v.ExtractorVersion = "different-runtime" },
	} {
		changed := *plan
		change(&changed.Input)
		_, err := acceptCheckpointHandoff(f, &changed)
		require.ErrorIs(t, err, models.ErrEnrichmentConflict, "changed runtime requires its own preview")
	}
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceEvidence.AddPostIdentifier(ctx, plan.Target.PostUUID, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "review-added"}, plan.PostRevision)
	}))
	_, err := acceptCheckpointHandoff(f, plan)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict, "a post change invalidates the previously reviewed plan")
	plan = checkpointHandoffPreview(t, f, input)
	definition := plan.Collection.SourceCollectionDefinition
	definition.Label = "Changed after review"
	_, err = reviseEnrichmentCollection(f.repo, plan.Collection.UUID, plan.Collection.Revision, models.SourceCollectionInput{SourceCollectionDefinition: definition, Origin: "review"})
	require.NoError(t, err)
	_, err = acceptCheckpointHandoff(f, plan)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	input.CollectionRevision++
	next := checkpointHandoffPreview(t, f, input)
	_, err = scheduleEnrichment(f.repo, &next.Target, models.EnrichmentSchedule{State: "review", Priority: 5, Reason: "owner_changed"}, handoffTime(next))
	require.NoError(t, err)
	_, err = acceptCheckpointHandoff(f, next)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
}

func TestCheckpointHandoffCaughtLateErrorCannotCommitPartialReview(t *testing.T) {
	f, input := checkpointHandoffFixture(t)
	plan := checkpointHandoffPreview(t, f, input)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER handoff_late_failure AFTER INSERT ON checkpoint_handoffs
 BEGIN DELETE FROM checkpoint_evidence_captures WHERE acceptance_uuid=NEW.evidence_uuid; END`)
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationCheckpointImport.AcceptHandoff(ctx, input, plan.PlanSHA256, handoffTime(plan))
		require.ErrorIs(t, err, models.ErrSourcePayloadCorrupt)
		return nil
	})
	require.ErrorIs(t, err, models.ErrEnrichmentAtomic)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM checkpoint_handoffs"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM checkpoint_evidence_captures"))
}

func TestCheckpointHandoffCorruptSeedRefusesStartupWithoutWrites(t *testing.T) {
	f, input := checkpointHandoffFixture(t)
	plan := checkpointHandoffPreview(t, f, input)
	_, err := acceptCheckpointHandoff(f, plan)
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	_, err = raw.Exec(`DROP TRIGGER checkpoint_handoff_immutable; UPDATE checkpoint_handoffs SET seed_sha256=?`, strings.Repeat("0", 64))
	require.NoError(t, err)
	_, err = raw.Exec(`CREATE TRIGGER checkpoint_handoff_immutable BEFORE UPDATE ON checkpoint_handoffs BEGIN SELECT RAISE(ABORT,'immutable'); END`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Error(t, f.db.Open(f.db.DatabasePath()))
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestCheckpointHandoffMigrationPreservesSchema63AndRollsBackCollision(t *testing.T) {
	for _, scenario := range []string{"upgrade", "collision"} {
		t.Run(scenario, func(t *testing.T) {
			f, input := checkpointHandoffFixture(t)
			plan := checkpointHandoffPreview(t, f, input)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			defer raw.Close()
			before := map[string][][]any{}
			for _, table := range []string{"automation_snapshot_records", "automation_checkpoint_bodies", "checkpoint_evidence_acceptances", "checkpoint_evidence_captures", "source_captures", "source_capture_contexts", "enrichment_targets"} {
				before[table] = albumJobRows(t, raw, table)
			}
			removeCheckpointHandoffSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000063")
			require.NoError(t, err)
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(path), &needed))
			if scenario == "collision" {
				_, err = raw.Exec("CREATE TABLE checkpoint_handoffs(original TEXT); INSERT INTO checkpoint_handoffs VALUES('retained')")
				require.NoError(t, err)
				require.Error(t, f.db.RunAllMigrations())
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000064"))
				require.Equal(t, [][]any{{"retained"}}, albumJobRows(t, raw, "checkpoint_handoffs"))
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
				f.repo = f.db.Repository()
				require.Equal(t, plan, checkpointHandoffPreview(t, f, input))
			}
			for table, values := range before {
				require.Equal(t, values, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
