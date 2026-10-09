package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func metadataRepair(kind string) models.WorkerPolicyUpgradeInput {
	return models.WorkerPolicyUpgradeInput{RequestUUID: uuid.NewString(), Kind: kind,
		OriginalPolicySHA256: strings.Repeat("a", 64), ExpectedPolicySHA256: strings.Repeat("a", 64),
		PolicySHA256: strings.Repeat("b", 64), Reason: "Compatible worker repair; retain observations and source coverage"}
}

func approveMetadataRepair(t *testing.T, repo models.Repository, now time.Time, input models.WorkerPolicyUpgradeInput) (*models.WorkerPolicyUpgrade, error) {
	t.Helper()
	var result *models.WorkerPolicyUpgrade
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = repo.WorkerPolicy.Upgrade(ctx, input, now)
		return err
	})
	return result, err
}

func TestMetadataWorkerRepairPreservesCheckpointAdmissionAndAttemptPolicy(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	checkpoint, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	input := metadataRepair(models.ArchiveJobEnrichPost)
	_, err = approveMetadataRepair(t, f.repo, f.now, input)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict, "running jobs cannot change executable authority")
	_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "worker_failed")
	require.NoError(t, err)
	before, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	receipt, err := approveMetadataRepair(t, f.repo, f.now, input)
	require.NoError(t, err)
	replay, err := approveMetadataRepair(t, f.repo, f.now, input)
	require.NoError(t, err)
	require.Equal(t, receipt, replay)
	changed := input
	changed.Reason = "Different request"
	_, err = approveMetadataRepair(t, f.repo, f.now, changed)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	changed.RequestUUID = uuid.NewString()
	_, err = approveMetadataRepair(t, f.repo, f.now, changed)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict, "stale approval cannot replace the current execution policy")
	after, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	before.ExecutionPolicySHA256 = input.PolicySHA256
	require.Equal(t, before, after, "upgrade changes no job state, retry time, progress or original arguments")
	originalReceipt, err := f.worker.Admit(t.Context(), f.tokens[0], f.target.UUID, f.target.Revision, input.OriginalPolicySHA256, "1.32.15-dev")
	require.NoError(t, err)
	require.Equal(t, after, originalReceipt)
	_, err = f.worker.Claim(t.Context(), f.tokens[0], job.UUID, after.Revision, uuid.NewString(), input.OriginalPolicySHA256, "1.32.15-dev", time.Minute)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	tooEarly, err := f.worker.Claim(t.Context(), f.tokens[0], job.UUID, after.Revision, uuid.NewString(), input.PolicySHA256, "1.32.15-dev", time.Minute)
	require.NoError(t, err)
	require.Nil(t, tooEarly)
	f.now = after.AvailableAt.Add(2 * time.Hour)
	ready, err := f.worker.ReadyJobs(t.Context(), f.tokens[0], f.collection.UUID, input.PolicySHA256, "1.32.15-dev", 0, 10)
	require.NoError(t, err)
	require.Equal(t, []models.EnrichmentJobCandidate{{Sequence: job.Sequence, UUID: job.UUID}}, ready)
	oldReady, err := f.worker.ReadyJobs(t.Context(), f.tokens[0], f.collection.UUID, input.OriginalPolicySHA256, "1.32.15-dev", 0, 10)
	require.NoError(t, err)
	require.Empty(t, oldReady)
	second, err := f.worker.Claim(t.Context(), f.tokens[0], job.UUID, after.Revision, uuid.NewString(), input.PolicySHA256, "1.32.15-dev", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, second)
	stillSaved, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, *checkpoint, stillSaved.EnrichmentCheckpointReceipt)
	final, err := f.worker.Checkpoint(t.Context(), f.tokens[0], second.Lease(), checkpoint.Revision, f.complete)
	require.NoError(t, err)
	_, err = f.worker.Publish(t.Context(), f.tokens[0], second.Lease(), final.Revision, final.Digest)
	require.NoError(t, err)
	// Future approvals cannot relabel work already executed by the prior code.
	changed = input
	changed.RequestUUID, changed.ExpectedPolicySHA256, changed.PolicySHA256 = uuid.NewString(), input.PolicySHA256, strings.Repeat("c", 64)
	_, err = approveMetadataRepair(t, f.repo, f.now, changed)
	require.NoError(t, err)
	finished, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", finished.State)
	require.Equal(t, input.PolicySHA256, finished.ExecutionPolicySHA256)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		attempts, err := f.repo.ArchiveJob.Attempts(ctx, job.UUID, 0, 10)
		require.NoError(t, err)
		require.Len(t, attempts, 2)
		require.Equal(t, input.OriginalPolicySHA256, attempts[0].PolicySHA256)
		require.Equal(t, input.PolicySHA256, attempts[1].PolicySHA256)
		otherKind, err := f.repo.WorkerPolicy.Resolve(ctx, models.ArchiveJobListAccount, input.OriginalPolicySHA256)
		require.NoError(t, err)
		require.Equal(t, input.OriginalPolicySHA256, otherKind)
		otherProfile, err := f.repo.WorkerPolicy.Resolve(ctx, input.Kind, strings.Repeat("d", 64))
		require.NoError(t, err)
		require.Equal(t, strings.Repeat("d", 64), otherProfile)
		return nil
	}))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE metadata_worker_policy_upgrades SET reason='rewrite' WHERE request_uuid=?", input.RequestUUID)
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("DELETE FROM metadata_worker_attempt_policies WHERE job_uuid=?", job.UUID)
	require.ErrorContains(t, err, "retained")
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	anonymous := openRawDB(t, output)
	defer anonymous.Close()
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM metadata_worker_policy_upgrades"))
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM metadata_worker_attempt_policies"))
}

func TestMetadataWorkerRepairMigrationPreservesExistingCheckpoint(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "worker_failed")
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	removeSourceThreadSchema(t, raw)
	_, err = raw.Exec(`DROP TABLE metadata_worker_attempt_policies;
DROP TABLE metadata_worker_policy_upgrades;
DELETE FROM native_migration_history WHERE version=1000104;
UPDATE schema_migrations SET version=1000103`)
	require.NoError(t, err)
	tables := []string{"archive_jobs", "archive_job_attempts", "enrichment_checkpoints", "enrichment_checkpoint_records", "enrichment_checkpoint_receipts"}
	before := map[string][][]any{}
	for _, table := range tables {
		before[table] = albumJobRows(t, raw, table)
	}
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	for _, table := range tables {
		require.Equal(t, before[table], albumJobRows(t, raw, table), table)
	}
	repo := f.db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		attempts, err := repo.ArchiveJob.Attempts(ctx, job.UUID, 0, 10)
		require.NoError(t, err)
		require.Equal(t, strings.Repeat("a", 64), attempts[0].PolicySHA256)
		return nil
	}))
}

func TestMetadataWorkerRepairResumesListingCursorAndFuturePages(t *testing.T) {
	f := newListingFixture(t)
	f.admit(t)
	first := f.claim(t, 0)
	cursor := map[string]string{"after": "t3_next"}
	_, err := f.append(t, models.DiscoveryJobLease{ArchiveJobLease: first.Lease(), ProducerUUID: f.producers[0].UUID}, 1,
		listingContinuation(t, f.page, nil, cursor, false))
	require.NoError(t, err)
	f.now = f.now.Add(2 * time.Hour)
	input := metadataRepair(models.ArchiveJobListAccount)
	_, err = approveMetadataRepair(t, f.repo, f.now, input)
	require.NoError(t, err)
	worker := ingest.NewDiscoveryCoordinator(f.service)
	worker.Now = func() time.Time { return f.now }
	job, err := worker.Admit(t.Context(), f.tokens[0], f.listing.UUID, f.listing.Digest, input.PolicySHA256, f.listing.ExtractorVersion)
	require.NoError(t, err)
	described, err := worker.Describe(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, f.listing, described.Listing)
	require.Equal(t, cursor, described.Cursor)
	require.Equal(t, input.PolicySHA256, job.ExecutionPolicySHA256)
	second, err := worker.Claim(t.Context(), f.tokens[0], job.UUID, job.Revision, uuid.NewString(), input.PolicySHA256, f.listing.ExtractorVersion, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, second)
	_, err = f.append(t, models.DiscoveryJobLease{ArchiveJobLease: second.Lease(), ProducerUUID: f.producers[0].UUID}, 2,
		listingContinuation(t, f.page, cursor, nil, true))
	require.NoError(t, err)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		pages, err := f.repo.DiscoveryJob.Pages(ctx, f.listing.UUID, 0, 10)
		require.NoError(t, err)
		require.Len(t, pages, 2)
		require.Equal(t, first.UUID, pages[0].JobUUID)
		require.Equal(t, second.UUID, pages[1].JobUUID)
		old, err := f.repo.ArchiveJob.Find(ctx, first.UUID)
		require.NoError(t, err)
		require.Equal(t, input.OriginalPolicySHA256, old.ExecutionPolicySHA256)
		return nil
	}))
}

func TestMetadataWorkerRepairKeepsDetailReviewUnaccepted(t *testing.T) {
	f := newDetailFixture(t)
	job := f.admit(t)
	before := f.review(t)
	input := metadataRepair(models.ArchiveJobVerifyCandidate)
	input.OriginalPolicySHA256, input.ExpectedPolicySHA256 = f.input.PolicySHA256, f.input.PolicySHA256
	_, err := approveMetadataRepair(t, f.repo, f.now, input)
	require.NoError(t, err)
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, job.Arguments, current.Arguments)
	f.now = f.now.Add(2 * time.Hour)
	running, err := f.worker.Claim(t.Context(), f.tokens[0], job.UUID, current.Revision, uuid.NewString(), input.PolicySHA256, f.input.ExtractorVersion, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, running)
	require.Equal(t, before, f.review(t), "approving worker code must not accept a proposed post identity")
}
