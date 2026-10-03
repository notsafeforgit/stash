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

func removeCheckpointEvidenceSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP TABLE checkpoint_evidence_captures; DROP TABLE checkpoint_evidence_acceptances;
 DELETE FROM native_migration_history WHERE version=1000062`)
	require.NoError(t, err)
}

func checkpointEvidenceFixture(t *testing.T) (*automationSnapshotFixture, models.CheckpointEvidenceInput) {
	t.Helper()
	f := checkpointImportFixture(t, 1)
	advanceCheckpointImport(t, f, 0)
	input := models.CheckpointEvidenceInput{UUID: uuid.NewString(), SnapshotUUID: f.manifest.UUID, ManifestSHA256: f.sha}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.AutomationCheckpointImport.Records(ctx, f.manifest.UUID, 0, 1)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		source, err := f.repo.AutomationEnrichmentImport.Record(ctx, f.manifest.UUID, rows[0].Ordinal)
		require.NoError(t, err)
		require.NotNil(t, source.TargetRevision)
		input.Ordinal, input.TargetRevision = rows[0].Ordinal, *source.TargetRevision
		return nil
	}))
	return f, input
}

func checkpointEvidencePreview(t *testing.T, f *automationSnapshotFixture, input models.CheckpointEvidenceInput) *models.CheckpointEvidencePlan {
	t.Helper()
	var plan *models.CheckpointEvidencePlan
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		plan, err = f.repo.AutomationCheckpointImport.PreviewEvidence(ctx, input)
		return err
	}))
	return plan
}

func acceptCheckpointEvidence(f *automationSnapshotFixture, plan *models.CheckpointEvidencePlan) (*models.CheckpointEvidenceAcceptance, error) {
	var result *models.CheckpointEvidenceAcceptance
	err := f.repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.AutomationCheckpointImport.AcceptEvidence(ctx, plan.Input, plan.PlanSHA256, automationImportNow.Add(3*time.Hour))
		return err
	})
	return result, err
}

func TestCheckpointEvidenceAcceptancePreservesUndatedBodiesAndReviewHold(t *testing.T) {
	f, input := checkpointEvidenceFixture(t)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"automation_snapshot_records", "automation_enrichment_records", "automation_checkpoint_records", "automation_checkpoint_bodies",
		"enrichment_targets", "enrichment_target_history", "archive_jobs", "enrichment_job_attempts", "enrichment_checkpoints", "capture_publisher_decisions", "source_account_identifiers", "scenes", "images"} {
		before[table] = albumJobRows(t, raw, table)
	}
	plan := checkpointEvidencePreview(t, f, input)
	require.Len(t, plan.Captures, 2)
	require.Equal(t, 2, plan.RecordCount)
	require.Equal(t, 1, plan.PendingCount)
	require.Equal(t, 1, plan.UnscopedCount)
	require.True(t, plan.AssignPostID)
	require.Equal(t, "native:reddit", plan.PostNamespace)
	require.Equal(t, "receipt000", plan.PostValue)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM checkpoint_evidence_acceptances"))
	receipt, err := acceptCheckpointEvidence(f, plan)
	require.NoError(t, err)
	require.Equal(t, *plan, receipt.CheckpointEvidencePlan)
	for table, values := range before {
		require.Equal(t, values, albumJobRows(t, raw, table), table)
	}
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_captures WHERE origin='legacy-enrichment' AND captured_at IS NULL AND recorded_at IS NOT NULL"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM checkpoint_evidence_captures"))
	duplicate := *plan
	duplicate.Input.UUID = uuid.NewString()
	_, err = acceptCheckpointEvidence(f, &duplicate)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict, "a second review cannot replace the original acceptance")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, binding := range plan.Captures {
			capture, err := f.repo.SourceEvidence.FindCapture(ctx, binding.CaptureUUID)
			require.NoError(t, err)
			require.True(t, capture.CapturedAt.IsZero())
			require.True(t, capture.RecordedAt.Equal(receipt.CreatedAt))
			restored, err := archive.RestoreCapture(capture.Payload)
			require.NoError(t, err)
			if binding.BodyIndex == 0 {
				require.Contains(t, string(restored), "9007199254740993")
			} else {
				require.NotContains(t, string(restored), "source_extractor_url", "missing historical fields stay missing")
			}
		}
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	backup := filepath.Join(t.TempDir(), "checkpoint-evidence.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(backup))
	f.repo = f.db.Repository()
	replayed, err := acceptCheckpointEvidence(f, plan)
	require.NoError(t, err)
	require.Equal(t, receipt, replayed)
	_, err = scheduleEnrichment(f.repo, readEnrichmentTarget(t, f.repo, plan.Target.UUID), models.EnrichmentSchedule{State: "excluded", Priority: 5, Reason: "owner_excluded"}, automationImportNow.Add(4*time.Hour))
	require.NoError(t, err)
	replayed, err = acceptCheckpointEvidence(f, plan)
	require.NoError(t, err)
	require.Equal(t, receipt, replayed)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(backup), "historical acceptance survives later target decisions")
	anonPath := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anon, err := sqlite.NewAnonymiser(f.db, anonPath)
	require.NoError(t, err)
	require.NoError(t, anon.Anonymise(t.Context()))
	anonymous := openRawDB(t, anonPath)
	defer anonymous.Close()
	for _, table := range []string{"checkpoint_evidence_captures", "checkpoint_evidence_acceptances", "source_captures"} {
		require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM "+table))
	}
}

func TestCheckpointEvidenceRejectsStaleReviewAndRollsBackCaughtLateFailure(t *testing.T) {
	f, input := checkpointEvidenceFixture(t)
	plan := checkpointEvidencePreview(t, f, input)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := queryUint(t, raw, "SELECT count(*) FROM source_captures")
	forged := *plan
	forged.PlanSHA256 = strings.Repeat("0", 64)
	_, err := acceptCheckpointEvidence(f, &forged)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	_, err = raw.Exec(`CREATE TRIGGER evidence_late_failure BEFORE INSERT ON checkpoint_evidence_captures WHEN NEW.body_index=1 BEGIN SELECT RAISE(ABORT,'Injected late acceptance failure'); END`)
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationCheckpointImport.AcceptEvidence(ctx, input, plan.PlanSHA256, automationImportNow.Add(3*time.Hour))
		require.ErrorContains(t, err, "Injected late acceptance failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrEnrichmentAtomic)
	require.Equal(t, before, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM checkpoint_evidence_acceptances"))
	_, err = raw.Exec("DROP TRIGGER evidence_late_failure")
	require.NoError(t, err)
	_, err = scheduleEnrichment(f.repo, &plan.Target, models.EnrichmentSchedule{State: "review", Priority: 10, Reason: "owner_changed"}, automationImportNow.Add(3*time.Hour))
	require.NoError(t, err)
	_, err = acceptCheckpointEvidence(f, plan)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	require.Equal(t, before, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
}

func TestCheckpointEvidenceStartupRejectsMissingBindingWithoutWriting(t *testing.T) {
	f, input := checkpointEvidenceFixture(t)
	plan := checkpointEvidencePreview(t, f, input)
	_, err := acceptCheckpointEvidence(f, plan)
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	_, err = raw.Exec("DELETE FROM checkpoint_evidence_captures WHERE body_index=1")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Error(t, f.db.Open(f.db.DatabasePath()))
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestCheckpointEvidencePostIdentityRequiresUnambiguousReviewedBinding(t *testing.T) {
	for _, scenario := range []string{"same post", "other post", "different identifier", "changed after preview"} {
		t.Run(scenario, func(t *testing.T) {
			f, input := checkpointEvidenceFixture(t)
			plan := checkpointEvidencePreview(t, f, input)
			key := models.SourcePostIdentifier{Namespace: plan.PostNamespace, Value: plan.PostValue}
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				if scenario == "other post" {
					_, err := f.repo.SourceEvidence.EnsurePost(ctx, key, "")
					return err
				}
				if scenario == "different identifier" {
					key.Value = "another-post"
				}
				return f.repo.SourceEvidence.AddPostIdentifier(ctx, plan.Target.PostUUID, key, plan.PostRevision)
			}))
			if scenario == "changed after preview" {
				_, err := acceptCheckpointEvidence(f, plan)
				require.ErrorIs(t, err, models.ErrEnrichmentConflict)
			}
			var next *models.CheckpointEvidencePlan
			err := f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				var err error
				next, err = f.repo.AutomationCheckpointImport.PreviewEvidence(ctx, input)
				return err
			})
			if scenario == "other post" || scenario == "different identifier" {
				require.ErrorIs(t, err, models.ErrEnrichmentConflict)
				return
			}
			require.NoError(t, err)
			require.False(t, next.AssignPostID)
			_, err = acceptCheckpointEvidence(f, next)
			require.NoError(t, err)
			changed := *next
			changed.Input.ManifestSHA256 = strings.Repeat("0", 64)
			_, err = acceptCheckpointEvidence(f, &changed)
			require.ErrorIs(t, err, models.ErrEnrichmentConflict)
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
		})
	}
}

func TestCheckpointEvidenceMigrationPreservesStagingAndRollsBackCollision(t *testing.T) {
	for _, scenario := range []string{"upgrade", "collision"} {
		t.Run(scenario, func(t *testing.T) {
			f, input := checkpointEvidenceFixture(t)
			plan := checkpointEvidencePreview(t, f, input)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			defer raw.Close()
			before := map[string][][]any{}
			for _, table := range []string{"automation_snapshot_records", "automation_checkpoint_records", "automation_checkpoint_bodies", "source_captures", "enrichment_targets", "enrichment_target_history"} {
				before[table] = albumJobRows(t, raw, table)
			}
			removeCheckpointEvidenceSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000061")
			require.NoError(t, err)
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(path), &needed))
			if scenario == "collision" {
				_, err = raw.Exec("CREATE TABLE checkpoint_evidence_captures(original TEXT); INSERT INTO checkpoint_evidence_captures VALUES('retained')")
				require.NoError(t, err)
				require.Error(t, f.db.RunAllMigrations())
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='checkpoint_evidence_acceptances'"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000062"))
				require.Equal(t, [][]any{{"retained"}}, albumJobRows(t, raw, "checkpoint_evidence_captures"))
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
				f.repo = f.db.Repository()
				require.Equal(t, plan, checkpointEvidencePreview(t, f, input))
			}
			for table, values := range before {
				require.Equal(t, values, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
