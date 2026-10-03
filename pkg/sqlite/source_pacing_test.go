package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type pacingDownload struct {
	run         *models.SourceRun
	token       string
	coordinator *ingest.RunCoordinator
}

func newPacingDownload(t *testing.T, f *enrichmentExecutionFixture, url string) pacingDownload {
	t.Helper()
	binding, err := archive.ProbeMediaRoot(t.TempDir())
	require.NoError(t, err)
	root := putMediaRoot(t, f.repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Downloads", State: "active", Binding: binding}})
	collection := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Download feed", Kind: "feed", Namespace: "native:reddit", State: "active", TargetURL: url, RootUUID: &root.UUID, PathPrefix: "Feed"}})
	_, token, err := f.service.IssueCredential(t.Context(), f.producers[1].UUID, []models.IngestScope{{CollectionUUID: collection.UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	coordinator := ingest.NewRunCoordinator(f.service)
	coordinator.Now = func() time.Time { return f.now }
	run, err := coordinator.Submit(t.Context(), token, models.SourceRunRequest{RequestUUID: uuid.NewString(), CollectionUUID: collection.UUID,
		CollectionRevision: collection.Revision, Operation: "download", PolicySHA256: strings.Repeat("a", 64), Window: models.SourceWindow{Until: f.now}, CooldownSeconds: 5})
	require.NoError(t, err)
	return pacingDownload{run, token, coordinator}
}

func (d pacingDownload) claim(t *testing.T) *models.SourceRun {
	t.Helper()
	run, err := d.coordinator.Claim(t.Context(), d.token, d.run.UUID, uuid.NewString(), d.run.PolicySHA256, time.Minute)
	require.NoError(t, err)
	return run
}

func tryPacingEnrichment(t *testing.T, f *enrichmentExecutionFixture, job *models.ArchiveJob) *models.ArchiveJob {
	t.Helper()
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	claimed, err := f.worker.Claim(t.Context(), f.tokens[0], job.UUID, current.Revision, uuid.NewString(), strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
	require.NoError(t, err)
	return claimed
}

func TestSourcePacingDownloadPreferenceAndCrossProducerExclusion(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	download := newPacingDownload(t, f, "https://old.reddit.com/user/different")
	require.Nil(t, tryPacingEnrichment(t, f, job), "a runnable download gets the first turn")
	running := download.claim(t)
	require.NotNil(t, running)
	replay, err := download.coordinator.Claim(t.Context(), download.token, running.UUID, running.OwnerUUID, running.PolicySHA256, time.Minute)
	require.NoError(t, err)
	require.Equal(t, running, replay)
	require.Nil(t, tryPacingEnrichment(t, f, job))
	_, err = download.coordinator.Finish(t.Context(), download.token, running.Lease(), models.SourceRunOutcome{State: "succeeded"})
	require.NoError(t, err)
	enriching := tryPacingEnrichment(t, f, job)
	require.NotNil(t, enriching)
	second := newPacingDownload(t, f, "https://www.reddit.com/user/another")
	require.Nil(t, second.claim(t), "a download cannot bypass an already owned enrichment attempt")
	other := newPacingDownload(t, f, "https://x.com/another")
	require.NotNil(t, other.claim(t), "independent services remain runnable")
}

func TestSourcePacingSimultaneousClaimsCannotOverlap(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	download := newPacingDownload(t, f, "https://www.reddit.com/user/different")
	var enrichment *models.ArchiveJob
	var run *models.SourceRun
	var enrichErr, downloadErr error
	start := make(chan struct{})
	var group sync.WaitGroup
	group.Go(func() {
		<-start
		enrichment, enrichErr = f.worker.Claim(t.Context(), f.tokens[0], job.UUID, job.Revision, uuid.NewString(), strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
	})
	group.Go(func() {
		<-start
		run, downloadErr = download.coordinator.Claim(t.Context(), download.token, download.run.UUID, uuid.NewString(), download.run.PolicySHA256, time.Minute)
	})
	close(start)
	group.Wait()
	require.NoError(t, enrichErr)
	require.NoError(t, downloadErr)
	require.Nil(t, enrichment)
	require.NotNil(t, run)
}

func TestSourcePacingFailureCooldownAndOldReceiptReplay(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	receipt, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "rate_limited")
	require.NoError(t, err)
	download := newPacingDownload(t, f, "https://www.reddit.com/user/different")
	require.Nil(t, download.claim(t))
	until := f.now.Add(time.Hour)
	f.now = f.now.Add(30 * time.Minute)
	replayed, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "rate_limited")
	require.NoError(t, err)
	require.Equal(t, receipt, replayed)
	f.now = until
	require.NotNil(t, download.claim(t), "replaying a receipt must not extend the original cooldown")
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, until, current.AvailableAt)
}

func TestSourcePacingSeparateEnrichmentCollectionsShareServiceOwnership(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	one := f.admit(t)
	collection := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Second feed", Kind: "feed", Namespace: "native:reddit", State: "active", TargetURL: "https://old.reddit.com/user/two"}})
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "def456"}, "")
	url, err := observePostURL(f.repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: "https://old.reddit.com/comments/def456"})
	require.NoError(t, err)
	target := retainEnrichment(t, f.repo, models.EnrichmentTargetInput{PostUUID: post.UUID, URLUUID: url.URLUUID, CollectionUUID: collection.UUID,
		CollectionRevision: collection.Revision, Policy: models.EnrichmentGalleryMetadataV1, Origin: "review"}, models.EnrichmentSchedule{State: "pending"}, f.now)
	_, token, err := f.service.IssueCredential(t.Context(), f.producers[1].UUID, []models.IngestScope{{CollectionUUID: collection.UUID}}, nil)
	require.NoError(t, err)
	two, err := f.worker.Admit(t.Context(), token, target.UUID, target.Revision, strings.Repeat("a", 64), "1.32.15-dev")
	require.NoError(t, err)
	var group sync.WaitGroup
	start := make(chan struct{})
	results := make([]*models.ArchiveJob, 2)
	errs := make([]error, 2)
	for i, item := range []struct {
		job   *models.ArchiveJob
		token string
	}{{one, f.tokens[0]}, {two, token}} {
		group.Go(func() {
			<-start
			results[i], errs[i] = f.worker.Claim(t.Context(), item.token, item.job.UUID, item.job.Revision, uuid.NewString(), strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
		})
	}
	close(start)
	group.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.True(t, (results[0] == nil) != (results[1] == nil), "exactly one service owner may start")
}

func TestSourcePacingDownloadFailureAlsoDelaysEnrichment(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	download := newPacingDownload(t, f, "https://www.reddit.com/user/different")
	running := download.claim(t)
	require.NotNil(t, running)
	_, err := download.coordinator.Finish(t.Context(), download.token, running.Lease(), models.SourceRunOutcome{State: "deferred", ErrorCode: "authentication"})
	require.NoError(t, err)
	require.Nil(t, tryPacingEnrichment(t, f, job))
	f.now = f.now.Add(24 * time.Hour)
	require.NotNil(t, tryPacingEnrichment(t, f, job), "the deferred download no longer reserves the source")
}

func TestSourcePacingChildFailureDoesNotPauseParentWebsite(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "timeout")
	require.NoError(t, err)
	parent := newPacingDownload(t, f, "https://www.reddit.com/user/different")
	parentRun := parent.claim(t)
	require.NotNil(t, parentRun, "retained failure is from Redgifs, not Reddit")
	_, err = parent.coordinator.Finish(t.Context(), parent.token, parentRun.Lease(), models.SourceRunOutcome{State: "succeeded"})
	require.NoError(t, err)
	child := newPacingDownload(t, f, "https://www.redgifs.com/users/example")
	require.Nil(t, child.claim(t))
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, f.now.Add(time.Hour), current.AvailableAt)
	// Remove the queued download so its priority cannot hide the cooldown check.
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceRun.Review(ctx, child.run.UUID, child.run.Revision, "cancel", f.now)
		return err
	}))
	// A different worker can discover a longer child-service outage later.
	f.now = f.now.Add(30 * time.Minute)
	childFailureAt := f.now
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := f.db.ExecSQL(ctx, "UPDATE source_pacing SET available_at_ms=? WHERE scope='service:redgifs'", []any{f.now.Add(time.Hour).UnixMilli()})
		return err
	}))
	f.now = current.AvailableAt
	require.Nil(t, tryPacingEnrichment(t, f, job), "a resumed job observes a newer child cooldown")
	f.now = childFailureAt.Add(time.Hour)
	require.NotNil(t, tryPacingEnrichment(t, f, job))
}

func TestSourcePacingExpiryReleasesServiceWithoutReplacingMetadata(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	download := newPacingDownload(t, f, "https://www.reddit.com/user/different")
	require.Nil(t, download.claim(t))
	f.now = running.LeaseUntil.Add(time.Second)
	require.NotNil(t, download.claim(t), "download claim recovers the expired enrichment owner atomically")
	retained, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, *head, retained.EnrichmentCheckpointReceipt)
}

func TestSourcePacingLinkedServiceReservationIsFencedAndReleased(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	childURL := "https://www.redgifs.com/watch/example"
	ready, err := f.worker.ReserveSource(t.Context(), f.tokens[0], running.Lease(), childURL)
	require.NoError(t, err)
	require.True(t, ready)
	ready, err = f.worker.ReserveSource(t.Context(), f.tokens[0], running.Lease(), childURL)
	require.NoError(t, err)
	require.True(t, ready, "a lost reservation response replays under the same fence")
	download := newPacingDownload(t, f, childURL)
	require.Nil(t, download.claim(t), "the linked service stays reserved throughout the attempt")
	ready, err = f.worker.ReserveSource(t.Context(), f.tokens[0], running.Lease(), childURL)
	require.NoError(t, err)
	require.True(t, ready, "a newly queued download cannot steal an existing reservation")
	_, err = f.worker.ReserveSource(t.Context(), f.tokens[1], running.Lease(), childURL)
	require.Error(t, err, "another producer cannot use this attempt")
	_, err = f.worker.ReserveSource(t.Context(), f.tokens[0], running.Lease(), "https://unrelated.invalid/private")
	require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "source_busy")
	require.NoError(t, err)
	require.NotNil(t, download.claim(t), "a finished attempt no longer holds its child service")
	_, err = f.worker.ReserveSource(t.Context(), f.tokens[0], running.Lease(), childURL)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
}

func TestSourcePacingBusyChildRetainsParentThenReservesPendingServiceOnRetry(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	download := newPacingDownload(t, f, "https://www.redgifs.com/watch/example")
	downloadRun := download.claim(t)
	require.NotNil(t, downloadRun)
	ready, err := f.worker.ReserveSource(t.Context(), f.tokens[0], running.Lease(), "https://www.redgifs.com/watch/other")
	require.NoError(t, err)
	require.False(t, ready)
	body := []byte(strings.ReplaceAll(string(f.initial), `"timeout"`, `"source_busy"`))
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, body)
	require.NoError(t, err)
	_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "source_busy")
	require.NoError(t, err)
	_, err = download.coordinator.Finish(t.Context(), download.token, downloadRun.Lease(), models.SourceRunOutcome{State: "succeeded"})
	require.NoError(t, err)
	queued, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	f.now = queued.AvailableAt
	retried := tryPacingEnrichment(t, f, job)
	require.NotNil(t, retried)
	second := newPacingDownload(t, f, "https://www.redgifs.com/watch/next")
	require.Nil(t, second.claim(t), "claim reserves the retained child before its next fetch")
	retained, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, *head, retained.EnrichmentCheckpointReceipt)
}

func TestSourcePacingChildReservationFailureRollsBack(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("CREATE TRIGGER child_pacing_failure BEFORE UPDATE OF last_started_at_ms ON source_pacing BEGIN SELECT RAISE(ABORT,'fixture'); END")
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, failure := f.repo.EnrichmentJob.ReserveSource(ctx, models.EnrichmentJobLease{ArchiveJobLease: running.Lease(), ProducerUUID: f.producers[0].UUID}, "https://redgifs.com/watch/example", f.now)
		require.Error(t, failure)
		return nil
	})
	require.ErrorIs(t, err, models.ErrSourcePacingAtomic)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_attempt_pacing WHERE scope='service:redgifs'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_pacing WHERE scope='service:redgifs'"))
}

func TestSourcePacingFailureRollbackWhenCallerCatchesWriteError(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("CREATE TRIGGER pacing_finish_failure BEFORE UPDATE ON archive_jobs BEGIN SELECT RAISE(ABORT,'fixture'); END")
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, failure := f.repo.ArchiveJob.Finish(ctx, running.Lease(), f.now, models.ArchiveJobOutcome{State: "retry", RetryAt: f.now.Add(time.Minute), ErrorCode: "rate_limited", Result: []byte(`{}`)})
		require.Error(t, failure)
		return nil
	})
	require.ErrorIs(t, err, models.ErrSourcePacingAtomic)
	require.Zero(t, queryUint(t, raw, "SELECT available_at_ms FROM source_pacing WHERE scope='service:reddit'"))
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.Equal(t, running, current)
}

func TestSourcePacingFailedClaimCannotCommitPartialOwnership(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	download := newPacingDownload(t, f, "https://www.reddit.com/user/different")
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("CREATE TRIGGER pacing_start_failure BEFORE UPDATE OF last_started_at_ms ON source_pacing BEGIN SELECT RAISE(ABORT,'fixture'); END")
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, failure := f.repo.SourceRun.Claim(ctx, download.run.UUID, f.producers[1].UUID, uuid.NewString(), download.run.PolicySHA256, f.now, time.Minute)
		require.Error(t, failure)
		return nil
	})
	require.ErrorIs(t, err, models.ErrSourcePacingAtomic)
	current, err := download.coordinator.Find(t.Context(), download.token, download.run.UUID)
	require.NoError(t, err)
	require.Equal(t, download.run, current)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_run_attempts"))
}

func removeSourcePacingSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeSourceRunServicesSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER source_run_pacing_bind; DROP TRIGGER enrichment_job_pacing_bind;
 DROP TRIGGER enrichment_attempt_pacing_bind; DROP TABLE enrichment_attempt_pacing;
 DROP TABLE source_run_pacing; DROP TABLE enrichment_job_pacing; DROP TABLE source_pacing;
 DELETE FROM native_migration_history WHERE version=1000054;`)
	require.NoError(t, err)
}

func TestSourcePacingMigrationPreservesPriorJobsAndRunningOwnership(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "upgrade", true: "collision"}[collision], func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
			require.NoError(t, err)
			download := newPacingDownload(t, f, "https://x.com/different")
			require.NotNil(t, download.claim(t))
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			defer raw.Close()
			removeSourcePacingSchema(t, raw)
			_, err = raw.Exec("UPDATE schema_migrations SET version=1000053")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"archive_jobs", "archive_job_attempts", "enrichment_job_targets", "enrichment_job_attempts", "enrichment_checkpoints", "enrichment_checkpoint_receipts", "source_runs", "source_run_attempts", "source_run_cooldowns"} {
				before[table] = albumJobRows(t, raw, table)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(path), &needed))
			if collision {
				_, err = raw.Exec("CREATE TABLE source_pacing(original TEXT); INSERT INTO source_pacing VALUES('retained')")
				require.NoError(t, err)
			}
			err = f.db.RunAllMigrations()
			if collision {
				require.Error(t, err)
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM source_pacing").Scan(&original))
				require.Equal(t, "retained", original)
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty=1"))
			} else {
				require.NoError(t, err)
				require.NoError(t, f.db.ReInitialise())
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_job_pacing WHERE scope='service:reddit'"))
				require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM enrichment_attempt_pacing"))
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_run_pacing WHERE scope='service:twitter'"))
				require.EqualValues(t, f.now.UnixMilli(), queryUint(t, raw, "SELECT last_started_at_ms FROM source_pacing WHERE scope='service:reddit'"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
