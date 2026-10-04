package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func migrationObject(t *testing.T, file, kind, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("migrations", file))
	require.NoError(t, err)
	end := `\n\);`
	if kind == "TRIGGER" {
		end = `END;`
	}
	ret := regexp.MustCompile(`(?s)CREATE ` + kind + ` ` + regexp.QuoteMeta(name) + `\b.*?` + end).FindString(string(body))
	require.NotEmpty(t, ret)
	return ret
}

func removeEnrichmentHandoffSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeSourceFileHistorySchema(t, raw)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs WHERE kind='post.enrich' AND json_extract(arguments,'$.version')!=1"), "historical migration fixtures must create historical jobs")
	_, err := raw.Exec(`DROP TABLE enrichment_job_seed_services; DROP TABLE enrichment_job_retained_records; DROP TABLE enrichment_handoff_jobs;
 DROP TRIGGER enrichment_job_target_scope; DROP TRIGGER enrichment_completion_capture_scope;
 DROP TRIGGER enrichment_checkpoint_published_delete;
 CREATE TEMP TABLE old_release_rows AS SELECT rowid AS saved_rowid,* FROM enrichment_checkpoint_releases;
 DROP TABLE enrichment_checkpoint_releases;`)
	require.NoError(t, err)
	_, err = raw.Exec(migrationObject(t, "1000053_enrichment_checkpoint_release.up.sql", "TABLE", "enrichment_checkpoint_releases"))
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO enrichment_checkpoint_releases(rowid,job_uuid,version,proof_sha256,checkpoint_bytes,unresolved,created_at)
 SELECT saved_rowid,job_uuid,version,proof_sha256,checkpoint_bytes,unresolved,created_at FROM old_release_rows;
 DROP TABLE old_release_rows; DELETE FROM native_migration_history WHERE version=1000065;`)
	require.NoError(t, err)
	for _, trigger := range []string{"enrichment_checkpoint_release_immutable", "enrichment_checkpoint_release_scope", "enrichment_checkpoint_published_delete"} {
		_, err := raw.Exec(migrationObject(t, "1000053_enrichment_checkpoint_release.up.sql", "TRIGGER", trigger))
		require.NoError(t, err)
	}
	_, err = raw.Exec(migrationObject(t, "1000051_enrichment_jobs.up.sql", "TRIGGER", "enrichment_job_target_scope") +
		migrationObject(t, "1000058_automation_enrichment.up.sql", "TRIGGER", "enrichment_completion_capture_scope"))
	require.NoError(t, err)
}

func (f *enrichmentExecutionFixture) admitV1(t *testing.T) *models.ArchiveJob {
	t.Helper()
	input, err := archive.PrepareEnrichmentJob(models.EnrichmentJobArguments{Version: 1, TargetUUID: f.target.UUID, TargetRevision: f.target.Revision,
		PostUUID: f.target.PostUUID, CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RootUUID: f.collection.RootUUID,
		PolicySHA256: strings.Repeat("a", 64), ExtractorVersion: "1.32.15-dev"})
	require.NoError(t, err)
	input.Priority, input.AvailableAt = f.target.Priority, f.target.NotBefore
	var job *models.ArchiveJob
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		job, err = f.repo.ArchiveJob.Submit(ctx, input, f.now, 10000)
		if err != nil {
			return err
		}
		return f.repo.EnrichmentJob.Bind(ctx, job.UUID, f.now)
	}))
	return job
}

type handoffExecutionFixture struct {
	*automationSnapshotFixture
	plan   *models.CheckpointHandoffPlan
	worker *ingest.EnrichmentCoordinator
	token  string
	now    time.Time
}

func newHandoffExecutionFixture(t *testing.T) *handoffExecutionFixture {
	t.Helper()
	base, input := checkpointHandoffFixture(t)
	f := &handoffExecutionFixture{automationSnapshotFixture: base, plan: checkpointHandoffPreview(t, base, input)}
	_, err := acceptCheckpointHandoff(base, f.plan)
	require.NoError(t, err)
	f.now = handoffTime(f.plan).Add(time.Minute)
	service := ingest.New(base.repo)
	f.worker = ingest.NewEnrichmentCoordinator(service)
	f.worker.Now = func() time.Time { return f.now }
	var producer *models.IngestProducer
	require.NoError(t, base.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = base.repo.Ingest.CreateProducer(ctx, "Reviewed checkpoint worker")
		return err
	}))
	_, f.token, err = service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: f.plan.Collection.UUID}}, nil)
	require.NoError(t, err)
	return f
}

func (f *handoffExecutionFixture) admit(t *testing.T) *models.ArchiveJob {
	t.Helper()
	job, err := f.worker.AdmitHandoff(t.Context(), f.token, f.plan.Input.UUID, f.plan.PlanSHA256, f.plan.Input.PolicySHA256, f.plan.Input.ExtractorVersion)
	require.NoError(t, err)
	require.NotNil(t, job)
	return job
}

func completeHandoffSeed(t *testing.T, seed *models.CheckpointHandoffSeed) []byte {
	t.Helper()
	value, err := archive.DecodeJSONObject(seed.Body, archive.MaxEnrichmentTranscriptBytes)
	require.NoError(t, err)
	parsed, err := archive.ParseEnrichmentTranscript(seed.Body)
	require.NoError(t, err)
	require.Len(t, parsed.Pending, 1)
	pending := parsed.Pending[0]
	value["records"] = append(value["records"].([]any), map[string]any{"kind": "media", "base": nil, "parent": pending.Parent, "removed": []any{},
		"observed_at": "2026-10-03T12:00:00Z", "patch": map[string]any{"category": "redgifs", "id": "recovered-child",
			"source_extractor_url": pending.URL, "_url": "https://media.invalid/full.mp4"}})
	value["pending"] = []any{}
	body, err := archive.EncodeSourceJSON(value)
	require.NoError(t, err)
	return body
}

func TestEnrichmentHandoffResumesOriginalEvidencePublishesAndRecovers(t *testing.T) {
	f := newHandoffExecutionFixture(t)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	originals := albumJobRows(t, raw, "source_captures")
	job := f.admit(t)
	work, err := archive.DecodeEnrichmentJob(job)
	require.NoError(t, err)
	require.Equal(t, 2, work.Version)
	require.Equal(t, f.plan.Input.UUID, work.Handoff.UUID)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_checkpoints"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_checkpoint_receipts"))
	require.Equal(t, originals, albumJobRows(t, raw, "source_captures"))
	replay := f.admit(t)
	require.Equal(t, job, replay)
	seed, err := f.worker.Seed(t.Context(), f.token, job.UUID)
	require.NoError(t, err)
	require.Equal(t, f.plan.SeedSHA256, seed.SHA256)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()), "the queued reviewed job survives restart without a fake checkpoint")
	running, err := f.worker.Claim(t.Context(), f.token, job.UUID, job.Revision, uuid.NewString(), work.PolicySHA256, work.ExtractorVersion, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, running)
	body := completeHandoffSeed(t, seed)
	head, err := f.worker.Checkpoint(t.Context(), f.token, running.Lease(), 0, body)
	require.NoError(t, err)
	ack, err := f.worker.Checkpoint(t.Context(), f.token, running.Lease(), 0, body)
	require.NoError(t, err)
	require.Equal(t, head, ack)
	publication, err := f.worker.Publish(t.Context(), f.token, running.Lease(), head.Revision, head.Digest)
	require.NoError(t, err)
	require.Equal(t, f.plan.RetainedCount+1, publication.CaptureCount)
	replayed, err := f.worker.Publish(t.Context(), f.token, running.Lease(), head.Revision, head.Digest)
	require.NoError(t, err)
	require.Equal(t, publication, replayed)
	release, err := f.worker.CheckpointRelease(t.Context(), f.token, job.UUID)
	require.NoError(t, err)
	require.Equal(t, 2, release.Version)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_checkpoints"))
	require.EqualValues(t, f.plan.RetainedCount, queryUint(t, raw, "SELECT count(*) FROM source_captures WHERE origin='legacy-enrichment' AND captured_at IS NULL AND recorded_at IS NOT NULL"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_capture_contexts"))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		records, err := f.repo.EnrichmentJob.PublishedRecords(ctx, job.UUID, -1, 100)
		require.NoError(t, err)
		for i, record := range records {
			capture, err := f.repo.SourceEvidence.FindCapture(ctx, record.CaptureUUID)
			require.NoError(t, err)
			if i < f.plan.RetainedCount {
				require.Equal(t, record.CaptureUUID, *record.RetainedCapture)
				require.True(t, capture.CapturedAt.IsZero())
				require.True(t, capture.RecordedAt.Equal(f.plan.EvidenceCreatedAt))
			} else {
				require.Nil(t, record.RetainedCapture)
				require.Equal(t, "2026-10-03T12:00:00Z", capture.CapturedAt.UTC().Format(time.RFC3339))
				require.Len(t, capture.Contexts, 1)
			}
		}
		return nil
	}))
	backup := filepath.Join(t.TempDir(), "handoff.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(backup))
	final := f.admit(t)
	require.Equal(t, job.UUID, final.UUID)
	require.Equal(t, "succeeded", final.State)
	anon, err := sqlite.NewAnonymiser(f.db, filepath.Join(t.TempDir(), "anonymous.sqlite"))
	require.NoError(t, err)
	require.NoError(t, anon.Anonymise(t.Context()))
}

func TestEnrichmentHandoffRejectsChangedReviewAndRollsBackLateFailure(t *testing.T) {
	f := newHandoffExecutionFixture(t)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER handoff_job_late_failure BEFORE INSERT ON enrichment_job_seed_services BEGIN SELECT RAISE(ABORT,'injected handoff failure'); END`)
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.EnrichmentJob.AdmitHandoff(ctx, f.plan.Input.UUID, f.plan.PlanSHA256, f.now)
		require.ErrorContains(t, err, "injected handoff failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrEnrichmentAtomic)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_handoff_jobs"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs WHERE kind='post.enrich'"))
	require.Equal(t, f.plan.Target, *readEnrichmentTarget(t, f.repo, f.plan.Target.UUID))
	_, err = raw.Exec("DROP TRIGGER handoff_job_late_failure")
	require.NoError(t, err)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceEvidence.AddPostIdentifier(ctx, f.plan.Target.PostUUID, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "other-binding"}, f.plan.PostRevision)
	}))
	_, err = f.worker.AdmitHandoff(t.Context(), f.token, f.plan.Input.UUID, f.plan.PlanSHA256, f.plan.Input.PolicySHA256, f.plan.Input.ExtractorVersion)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
}

func TestEnrichmentHandoffMigrationKeepsV1ReleasedProofs(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "upgrade", true: "collision"}[collision], func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			job := f.admitV1(t)
			running := f.claim(t, job.UUID, 0)
			head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
			require.NoError(t, err)
			_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.NoError(t, err)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			defer raw.Close()
			removeEnrichmentHandoffSchema(t, raw)
			_, err = raw.Exec("UPDATE schema_migrations SET version=1000064")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"archive_jobs", "source_captures", "enrichment_checkpoint_receipts", "enrichment_checkpoint_releases", "enrichment_published_records"} {
				before[table] = albumJobRows(t, raw, table)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(path), &needed))
			if collision {
				_, err := raw.Exec("CREATE TABLE enrichment_handoff_jobs(original TEXT); INSERT INTO enrichment_handoff_jobs VALUES('preserved')")
				require.NoError(t, err)
				require.Error(t, f.db.RunAllMigrations())
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000065"))
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
				require.NoError(t, f.db.Close())
				require.NoError(t, f.db.Open(path))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}

func TestEnrichmentHandoffFirstClaimWaitsForChildAndPreservesFairnessAcrossRestart(t *testing.T) {
	f := newHandoffExecutionFixture(t)
	job := f.admit(t)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("INSERT INTO source_pacing(scope,available_at_ms) VALUES('service:redgifs',?)", f.now.Add(time.Hour).UnixMilli())
	require.NoError(t, err)
	owner := uuid.NewString()
	claim := func() (*models.ArchiveJob, error) {
		return f.worker.Claim(t.Context(), f.token, job.UUID, job.Revision, owner, f.plan.Input.PolicySHA256, f.plan.Input.ExtractorVersion, time.Minute)
	}
	running, err := claim()
	require.NoError(t, err)
	require.Nil(t, running)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_checkpoints"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_enrichment_waiter_scopes WHERE scope='service:redgifs'"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.now = f.now.Add(time.Hour + time.Second)
	running, err = claim()
	require.NoError(t, err)
	require.NotNil(t, running)
	_, err = f.worker.Fail(t.Context(), f.token, running.Lease(), "timeout")
	require.NoError(t, err)
	require.Zero(t, queryUint(t, raw, "SELECT available_at_ms FROM source_pacing WHERE scope='service:reddit'"), "a child-only failure must not pause the parent website")
}

func TestEnrichmentHandoffCannotReplaceSeedOrFinishWithoutItsPublication(t *testing.T) {
	f := newHandoffExecutionFixture(t)
	job := f.admit(t)
	running, err := f.worker.Claim(t.Context(), f.token, job.UUID, job.Revision, uuid.NewString(), f.plan.Input.PolicySHA256, f.plan.Input.ExtractorVersion, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, running)
	seed, err := f.worker.Seed(t.Context(), f.token, job.UUID)
	require.NoError(t, err)
	value, err := archive.DecodeJSONObject(seed.Body, archive.MaxEnrichmentTranscriptBytes)
	require.NoError(t, err)
	value["records"].([]any)[0].(map[string]any)["patch"].(map[string]any)["title"] = "Unreviewed replacement"
	changed, err := archive.EncodeSourceJSON(value)
	require.NoError(t, err)
	_, err = f.worker.Checkpoint(t.Context(), f.token, running.Lease(), 0, changed)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	body, err := archive.ParseEnrichmentTranscript(seed.Body)
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var captures []string
		for _, record := range body.Records {
			captures = append(captures, *record.RetainedCapture)
			if err := f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.plan.Collection.UUID,
				CollectionRevision: f.plan.Collection.Revision, CaptureUUID: *record.RetainedCapture, CreatedAt: f.now}); err != nil {
				return err
			}
		}
		_, err := f.repo.EnrichmentWork.Complete(ctx, models.EnrichmentCompletionInput{UUID: uuid.NewString(), TargetUUID: f.plan.ReleasedTargetUUID,
			ExpectedRevision: f.plan.ReleasedRevision, CaptureUUIDs: captures}, f.now)
		return err
	})
	require.ErrorIs(t, err, models.ErrEnrichmentAtomic)
	require.Equal(t, "pending", readEnrichmentTarget(t, f.repo, f.plan.ReleasedTargetUUID).State)
}

func TestEnrichmentHandoffMissingProjectionRefusesStartupWithoutWrites(t *testing.T) {
	f := newHandoffExecutionFixture(t)
	f.admit(t)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	_, err := raw.Exec("DELETE FROM enrichment_job_seed_services")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Error(t, f.db.Open(path))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
