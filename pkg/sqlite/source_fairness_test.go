package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func removeSourceFairnessSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP TRIGGER source_service_turns_bind; DROP TRIGGER source_enrichment_waiter_end;
 DROP TABLE source_enrichment_waiter_scopes; DROP TABLE source_enrichment_waiters; DROP TABLE source_service_turns;
 DELETE FROM native_migration_history WHERE version=1000056;`)
	require.NoError(t, err)
}

func TestSourceFairnessDownloadPreferenceEndsAfterFourStarts(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	d := newPacingDownload(t, f, "https://reddit.com/user/first")
	require.Nil(t, tryPacingEnrichment(t, f, job))
	for i := 0; i < 4; i++ {
		if i != 0 {
			d = newPacingDownload(t, f, fmt.Sprintf("https://reddit.com/user/example%d", i))
		}
		r := d.claim(t)
		require.NotNil(t, r, "downloads initially take priority")
		require.Nil(t, tryPacingEnrichment(t, f, job), "live downloads are never displaced")
		_, err := d.coordinator.Finish(t.Context(), d.token, r.Lease(), models.SourceRunOutcome{State: "succeeded"})
		require.NoError(t, err)
	}
	d = newPacingDownload(t, f, "https://reddit.com/user/next")
	require.Nil(t, d.claim(t), "live enrichment interest gets the next turn")
	enriching := tryPacingEnrichment(t, f, job)
	require.NotNil(t, enriching)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT download_starts FROM source_service_turns WHERE scope='service:reddit'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_enrichment_waiters"))
	_, err := f.worker.Fail(t.Context(), f.tokens[0], enriching.Lease(), "not_found")
	require.NoError(t, err)
	require.NotNil(t, d.claim(t), "download preference resumes after the enrichment turn")
}

func TestSourceFairnessLiveWaitingAgesButAbandonedInterestExpires(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(fmt.Sprint(live), func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			job := f.admit(t)
			d := newPacingDownload(t, f, "https://reddit.com/user/example")
			require.Nil(t, tryPacingEnrichment(t, f, job))
			f.now = f.now.Add(time.Minute)
			if live {
				require.Nil(t, tryPacingEnrichment(t, f, job), "a refresh preserves the original wait time")
			}
			f.now = f.now.Add(time.Minute)
			if live {
				require.Nil(t, d.claim(t))
				require.NotNil(t, tryPacingEnrichment(t, f, job))
			} else {
				require.NotNil(t, d.claim(t), "an unattended queue does not reserve the service")
			}
		})
	}
}

func TestSourceFairnessOldestCollectionWinsConcurrentClaims(t *testing.T) {
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
	download := newPacingDownload(t, f, "https://reddit.com/user/download")
	require.Nil(t, tryPacingEnrichment(t, f, one))
	f.now = f.now.Add(time.Second)
	blocked, err := f.worker.Claim(t.Context(), token, two.UUID, two.Revision, uuid.NewString(), strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
	require.NoError(t, err)
	require.Nil(t, blocked)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE source_service_turns SET download_starts=4 WHERE scope='service:reddit'")
	require.NoError(t, err)
	start := make(chan struct{})
	var group sync.WaitGroup
	results := make([]*models.ArchiveJob, 2)
	errs := make([]error, 3)
	for i, item := range []struct {
		job   *models.ArchiveJob
		token string
	}{{one, f.tokens[0]}, {two, token}} {
		group.Go(func() {
			<-start
			results[i], errs[i] = f.worker.Claim(t.Context(), item.token, item.job.UUID, item.job.Revision, uuid.NewString(), strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
		})
	}
	var run *models.SourceRun
	group.Go(func() {
		<-start
		run, errs[2] = download.coordinator.Claim(t.Context(), download.token, download.run.UUID, uuid.NewString(), download.run.PolicySHA256, time.Minute)
	})
	close(start)
	group.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.NotNil(t, results[0], "the oldest collection owns the next turn")
	require.Nil(t, results[1])
	require.Nil(t, run)
	_, err = f.worker.Fail(t.Context(), f.tokens[0], results[0].Lease(), "not_found")
	require.NoError(t, err)
	require.NotNil(t, download.claim(t), "one enrichment start restores download preference")
}

func TestSourceFairnessHeldTargetDoesNotReserveService(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	download := newPacingDownload(t, f, "https://reddit.com/user/download")
	require.Nil(t, tryPacingEnrichment(t, f, job))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("UPDATE source_service_turns SET download_starts=4 WHERE scope='service:reddit'")
	require.NoError(t, err)
	_, err = scheduleEnrichment(f.repo, f.target, models.EnrichmentSchedule{State: "held", Reason: "review"}, f.now)
	require.NoError(t, err)
	require.NotNil(t, download.claim(t), "held metadata is ineligible even with live interest")
}

type fairnessClaimBeforeCommit struct {
	models.ArchiveJobReaderWriter
	beforeCommit func(context.Context) error
}

func (s fairnessClaimBeforeCommit) ClaimByID(ctx context.Context, id string, revision int64, owner string, now time.Time, duration time.Duration) (*models.ArchiveJob, error) {
	result, err := s.ArchiveJobReaderWriter.ClaimByID(ctx, id, revision, owner, now, duration)
	txn.AddPreCommitHook(ctx, s.beforeCommit)
	return result, err
}

func TestSourceFairnessBlockedClaimStillChecksAuthorizationAtCommit(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	newPacingDownload(t, f, "https://reddit.com/user/download")
	f.service.Repo.ArchiveJob = fairnessClaimBeforeCommit{f.repo.ArchiveJob, func(ctx context.Context) error {
		return f.repo.Ingest.RevokeCredential(ctx, f.tokens[0][7:43])
	}}
	_, err := f.worker.Claim(t.Context(), f.tokens[0], job.UUID, job.Revision, uuid.NewString(), strings.Repeat("a", 64), "1.32.15-dev", time.Minute)
	require.ErrorIs(t, err, ingest.ErrUnauthorized)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_enrichment_waiters"), "rejected claims cannot retain scheduling interest")
}

func TestSourceFairnessStartupChecksRetainedWaiterScopes(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	newPacingDownload(t, f, "https://reddit.com/user/download")
	require.Nil(t, tryPacingEnrichment(t, f, job))
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(path), "a valid blocked request survives restart")
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	_, err := raw.Exec("DELETE FROM source_enrichment_waiter_scopes WHERE job_uuid=?", job.UUID)
	require.NoError(t, err)
	require.ErrorIs(t, f.db.Open(path), models.ErrSourcePayloadCorrupt)
}

func TestSourceFairnessCoolingChildDoesNotReserveItsParent(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	r := f.claim(t, job.UUID, 0)
	_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], r.Lease(), 0, f.initial)
	require.NoError(t, err)
	_, err = f.worker.Fail(t.Context(), f.tokens[0], r.Lease(), "source_busy")
	require.NoError(t, err)
	current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	f.now = current.AvailableAt
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("INSERT OR IGNORE INTO source_pacing(scope) VALUES('service:redgifs'); UPDATE source_service_turns SET download_starts=4 WHERE scope='service:reddit'; UPDATE source_pacing SET available_at_ms=? WHERE scope='service:redgifs'", f.now.Add(time.Hour).UnixMilli())
	require.NoError(t, err)
	d := newPacingDownload(t, f, "https://reddit.com/user/example")
	require.Nil(t, tryPacingEnrichment(t, f, job))
	require.NotNil(t, d.claim(t), "a child cooldown makes the waiting metadata job temporarily ineligible")
}

func TestSourceFairnessYieldPreservesFailuresAndExactWindowProgress(t *testing.T) {
	f := newSourceRunFixture(t)
	r := f.claim(t, f.submit(t, f.request()))
	require.NotNil(t, r.TurnUntil)
	require.Equal(t, f.now.Add(5*time.Minute), *r.TurnUntil)
	f.now = f.now.Add(30 * time.Second)
	renewed, err := f.coordinator.Renew(t.Context(), f.token, r.Lease(), time.Minute)
	require.NoError(t, err)
	require.Equal(t, r.TurnUntil, renewed.TurnUntil, "heartbeat cannot extend the source time budget")
	progress := models.SourceRunProgress{ItemsSeen: 2, FilesCompleted: 1, Cursor: "saved"}
	_, err = f.coordinator.Progress(t.Context(), f.token, r.Lease(), progress)
	require.NoError(t, err)
	for i := 0; i < 10; i++ {
		queued, err := f.coordinator.Finish(t.Context(), f.token, r.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "source_turn_complete"})
		require.NoError(t, err)
		require.Zero(t, queued.Failures)
		require.Equal(t, "queued", queued.State)
		require.Nil(t, queued.TurnUntil)
		f.now = queued.AvailableAt
		r = f.claim(t, queued)
		require.NotNil(t, r)
		require.Equal(t, progress, r.Progress)
	}
}

func TestSourceFairnessCoolingDownloadDependencyDoesNotReserveParent(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	d := newPacingDownload(t, f, "https://reddit.com/user/example")
	r := d.claim(t)
	_, err := d.coordinator.ReserveSource(t.Context(), d.token, r.Lease(), "https://redgifs.com/watch/one")
	require.NoError(t, err)
	queued, err := d.coordinator.Finish(t.Context(), d.token, r.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "source_busy", ErrorScope: "service:redgifs"})
	require.NoError(t, err)
	f.now = queued.AvailableAt
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE source_pacing SET available_at_ms=? WHERE scope='service:redgifs'", f.now.Add(time.Hour).UnixMilli())
	require.NoError(t, err)
	job := f.admit(t)
	require.NotNil(t, tryPacingEnrichment(t, f, job), "a download waiting on another service cannot reserve Reddit")
}

func TestSourceFairnessRegistrationAndCountersRollbackTogether(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("CREATE TRIGGER break_turn BEFORE UPDATE ON source_service_turns BEGIN SELECT RAISE(ABORT,'fixture'); END")
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveJob.ClaimByID(ctx, job.UUID, job.Revision, f.producers[0].UUID, f.now, time.Minute)
		require.Error(t, err)
		return nil
	})
	require.Error(t, err)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_enrichment_waiters"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_job_attempts"))
}

func TestSourceFairnessMigrationPreservesExistingState(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	d := newPacingDownload(t, f, "https://reddit.com/user/example")
	require.NotNil(t, d.claim(t))
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeSourceFairnessSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000055")
	require.NoError(t, err)
	before := map[string][][]any{}
	for _, table := range []string{"source_runs", "source_run_attempts", "source_run_attempt_pacing", "source_run_attempt_failures", "source_pacing"} {
		before[table] = albumJobRows(t, raw, table)
	}
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(f.db.Open(path), &needed))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.EqualValues(t, queryUint(t, raw, "SELECT count(*) FROM source_pacing"), queryUint(t, raw, "SELECT count(*) FROM source_service_turns"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_enrichment_waiters"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
