package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

type durableJobFixture struct {
	db      *sqlite.Database
	repo    models.Repository
	service *job.Durable
	now     time.Time
}

func newDurableJobFixture(t *testing.T) *durableJobFixture {
	t.Helper()
	db, repo := archiveTestDatabase(t)
	f := &durableJobFixture{db: db, repo: repo, service: job.NewDurable(repo), now: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	f.service.Now = func() time.Time { return f.now }
	return f
}
func jobSubmission(key, resource string) models.ArchiveJobSubmission {
	return models.ArchiveJobSubmission{RequestUUID: uuid.NewString(), Kind: models.ArchiveJobVerifyMedia,
		WorkKey: ingest.Digest([]byte(key)), ResourceKey: ingest.Digest([]byte(resource)),
		Arguments: json.RawMessage(`{"relative_path":"fixture.jpg"}`), MaxAttempts: 3, Priority: 10}
}
func (f *durableJobFixture) submit(t *testing.T, input models.ArchiveJobSubmission) *models.ArchiveJob {
	t.Helper()
	ret, err := f.service.Submit(t.Context(), input)
	require.NoError(t, err)
	return ret
}
func (f *durableJobFixture) claim(t *testing.T, owner string) *models.ArchiveJob {
	t.Helper()
	ret, err := f.service.Claim(t.Context(), models.ArchiveJobVerifyMedia, owner, time.Minute)
	require.NoError(t, err)
	return ret
}
func (f *durableJobFixture) outcome(t *testing.T, current *models.ArchiveJob, outcome models.ArchiveJobOutcome) *models.ArchiveJob {
	t.Helper()
	ret, err := f.service.Publish(t.Context(), current.Lease(), func(context.Context, *models.ArchiveJob) (models.ArchiveJobOutcome, error) { return outcome, nil })
	require.NoError(t, err)
	return ret
}
func (f *durableJobFixture) find(t *testing.T, id string) *models.ArchiveJob {
	t.Helper()
	var ret *models.ArchiveJob
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error { var err error; ret, err = f.repo.ArchiveJob.Find(ctx, id); return err }))
	return ret
}

func TestArchiveJobsCoalesceBoundedWorkAndRetainSubmissionReceipts(t *testing.T) {
	f := newDurableJobFixture(t)
	f.service.MaxActive = 1
	input := jobSubmission("first", "destination")
	first := f.submit(t, input)
	f.now = f.now.Add(time.Second)
	require.Equal(t, first, f.submit(t, input))
	coalesced := input
	coalesced.RequestUUID = uuid.NewString()
	coalesced.Arguments = json.RawMessage(`{ "relative_path" : "fixture.jpg" }`)
	coalesced.Priority = 20
	second := f.submit(t, coalesced)
	require.Equal(t, first.UUID, second.UUID)
	require.Equal(t, 20, second.Priority)
	conflict := input
	conflict.Arguments = json.RawMessage(`{"relative_path":"other.jpg"}`)
	_, err := f.service.Submit(t.Context(), conflict)
	require.ErrorIs(t, err, models.ErrArchiveJobConflict)
	conflict.RequestUUID = uuid.NewString()
	_, err = f.service.Submit(t.Context(), conflict)
	require.ErrorIs(t, err, models.ErrArchiveJobConflict)
	_, err = f.service.Submit(t.Context(), jobSubmission("second", "elsewhere"))
	require.ErrorIs(t, err, models.ErrArchiveJobCapacity)
	claimed := f.claim(t, uuid.NewString())
	require.NotNil(t, claimed)
	completed := f.outcome(t, claimed, models.ArchiveJobOutcome{State: "succeeded", Result: json.RawMessage(`{"verified":true}`)})
	require.Equal(t, "succeeded", completed.State)
	require.Equal(t, completed, f.submit(t, input), "replay after completion must not schedule new work")
	require.Equal(t, completed, f.submit(t, coalesced), "coalesced submission also retains its original job")
	input.RequestUUID = uuid.NewString()
	third := f.submit(t, input)
	require.NotEqual(t, completed.UUID, third.UUID, "a new request may schedule work after the prior run completed")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		fromReceipt, err := f.repo.ArchiveJob.FindSubmission(ctx, coalesced.RequestUUID)
		require.NoError(t, err)
		require.Equal(t, completed, fromReceipt)
		pending, err := f.repo.ArchiveJob.List(ctx, models.ArchiveJobVerifyMedia, "queued", 0, 1)
		require.NoError(t, err)
		require.Equal(t, []models.ArchiveJob{*third}, pending)
		next, err := f.repo.ArchiveJob.List(ctx, models.ArchiveJobVerifyMedia, "queued", third.Sequence, 1)
		require.NoError(t, err)
		require.Empty(t, next)
		return nil
	}))
}

func TestArchiveJobsRestartRecoveryFencesOldWorkersAndBoundsRetries(t *testing.T) {
	f := newDurableJobFixture(t)
	input := jobSubmission("work", "destination")
	f.submit(t, input)
	first := f.claim(t, uuid.NewString())
	require.NotNil(t, first)
	require.EqualValues(t, 1, first.Fence)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.now = *first.LeaseUntil
	_, err := f.service.Renew(t.Context(), first.Lease(), time.Minute)
	require.ErrorIs(t, err, models.ErrArchiveJobLease, "expiry is exclusive")
	second := f.claim(t, uuid.NewString())
	require.NotNil(t, second)
	require.Equal(t, first.UUID, second.UUID)
	require.EqualValues(t, 2, second.Fence)
	called := false
	_, err = f.service.Publish(t.Context(), first.Lease(), func(context.Context, *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		called = true
		return models.ArchiveJobOutcome{}, nil
	})
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	require.False(t, called)
	wrongOwner := second.Lease()
	wrongOwner.OwnerUUID = uuid.NewString()
	_, err = f.service.Progress(t.Context(), wrongOwner, json.RawMessage(`{}`))
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	f.now = f.now.Add(30 * time.Second)
	renewed, err := f.service.Renew(t.Context(), second.Lease(), time.Minute)
	require.NoError(t, err)
	require.True(t, renewed.LeaseUntil.After(*second.LeaseUntil))
	progress, err := f.service.Progress(t.Context(), second.Lease(), json.RawMessage(`{"bytes":1234}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"bytes":1234}`, string(progress.Progress))
	retryAt := f.now.Add(time.Hour)
	queued := f.outcome(t, renewed, models.ArchiveJobOutcome{State: "retry", RetryAt: retryAt, ErrorCode: "mount_offline", Result: json.RawMessage(`{}`)})
	require.Equal(t, "queued", queued.State)
	repeat := input
	repeat.RequestUUID = uuid.NewString()
	repeat.Priority = 90
	coalesced := f.submit(t, repeat)
	require.Equal(t, retryAt, coalesced.AvailableAt, "a timer/manual repeat must not bypass retry backoff")
	require.Nil(t, f.claim(t, uuid.NewString()))
	f.now = retryAt
	third := f.claim(t, uuid.NewString())
	require.NotNil(t, third)
	require.EqualValues(t, 3, third.Fence)
	failed := f.outcome(t, third, models.ArchiveJobOutcome{State: "retry", RetryAt: f.now.Add(time.Minute), ErrorCode: "mount_offline", Result: json.RawMessage(`{}`)})
	require.Equal(t, "failed", failed.State)
	require.Nil(t, f.claim(t, uuid.NewString()), "exhausted attempts cannot loop forever")
	require.Equal(t, failed, f.submit(t, input))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		attempts, err := f.repo.ArchiveJob.Attempts(ctx, first.UUID, 0, 2)
		require.NoError(t, err)
		require.Len(t, attempts, 2)
		require.Equal(t, "expired", attempts[0].Outcome)
		require.Equal(t, first.OwnerUUID, attempts[0].OwnerUUID)
		require.Equal(t, "retry", attempts[1].Outcome)
		next, err := f.repo.ArchiveJob.Attempts(ctx, first.UUID, attempts[1].Fence, 2)
		require.NoError(t, err)
		require.Len(t, next, 1)
		require.Equal(t, "failed", next[0].Outcome)
		return nil
	}))
}

func TestArchiveJobsRecoveryIsBoundedAndExhaustedLeasesFail(t *testing.T) {
	f := newDurableJobFixture(t)
	for _, key := range []string{"first", "second", "third"} {
		input := jobSubmission(key, key)
		input.MaxAttempts = 1
		f.submit(t, input)
		require.NotNil(t, f.claim(t, uuid.NewString()))
	}
	f.now = f.now.Add(time.Minute)
	count, err := f.service.Recover(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		running, err := f.repo.ArchiveJob.List(ctx, models.ArchiveJobVerifyMedia, "running", 0, 10)
		require.NoError(t, err)
		require.Len(t, running, 1)
		failed, err := f.repo.ArchiveJob.List(ctx, models.ArchiveJobVerifyMedia, "failed", 0, 10)
		require.NoError(t, err)
		require.Len(t, failed, 2)
		return nil
	}))
	require.Nil(t, f.claim(t, uuid.NewString()), "claim recovers the remaining expired lease without exceeding its attempt limit")
	count, err = f.service.Recover(t.Context(), 2)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestArchiveJobsPriorityReadinessAndSharedDestination(t *testing.T) {
	f := newDurableJobFixture(t)
	a := jobSubmission("low", "shared")
	a.Priority = 1
	low := f.submit(t, a)
	b := jobSubmission("high", "shared")
	b.Priority = 90
	high := f.submit(t, b)
	other := f.submit(t, jobSubmission("other", "separate"))
	future := jobSubmission("future", "future")
	future.Priority = 100
	future.AvailableAt = f.now.Add(time.Hour)
	f.submit(t, future)
	first := f.claim(t, uuid.NewString())
	require.Equal(t, high.UUID, first.UUID)
	second := f.claim(t, uuid.NewString())
	require.Equal(t, other.UUID, second.UUID)
	require.Nil(t, f.claim(t, uuid.NewString()), "shared destination remains locked while its owner is active")
	f.outcome(t, first, models.ArchiveJobOutcome{State: "succeeded", Result: json.RawMessage(`{}`)})
	third := f.claim(t, uuid.NewString())
	require.Equal(t, low.UUID, third.UUID)
}

func TestArchiveJobSelectedClaimDoesNotSelectOrRecoverOtherWork(t *testing.T) {
	f := newDurableJobFixture(t)
	f.submit(t, jobSubmission("expired", "expired"))
	expired := f.claim(t, uuid.NewString())
	f.now = *expired.LeaseUntil
	highInput := jobSubmission("high", "high")
	highInput.Priority = 100
	high := f.submit(t, highInput)
	selected := f.submit(t, jobSubmission("selected", "selected"))

	claimed, err := f.service.ClaimByID(t.Context(), selected.UUID, selected.Revision, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, selected.UUID, claimed.UUID)
	require.Equal(t, "running", claimed.State)
	require.EqualValues(t, 1, claimed.Fence)
	require.Equal(t, high, f.find(t, high.UUID), "an explicit claim must not choose higher-priority unrelated work")
	require.Equal(t, expired, f.find(t, expired.UUID), "an explicit claim must not run global queue recovery")

	for _, unavailable := range []*models.ArchiveJob{claimed, expired} {
		ret, err := f.service.ClaimByID(t.Context(), unavailable.UUID, unavailable.Revision, uuid.NewString(), time.Minute)
		require.NoError(t, err)
		require.Nil(t, ret)
		require.Equal(t, unavailable, f.find(t, unavailable.UUID))
	}
	count, err := f.service.Recover(t.Context(), 100)
	require.NoError(t, err)
	require.Equal(t, 1, count, "trusted maintenance still recovers expired work")
	recovered := f.find(t, expired.UUID)
	claimed, err = f.service.ClaimByID(t.Context(), recovered.UUID, recovered.Revision, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.EqualValues(t, 2, claimed.Fence)
	_, err = f.service.Progress(t.Context(), expired.Lease(), json.RawMessage(`{}`))
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
}

func TestArchiveJobSelectedClaimRespectsSharedResourcesAndRetryDelay(t *testing.T) {
	f := newDurableJobFixture(t)
	otherKind := jobSubmission("album", "shared")
	otherKind.Kind = models.ArchiveJobBackfillAlbum
	f.submit(t, otherKind)
	blocker, err := f.service.Claim(t.Context(), otherKind.Kind, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, blocker)
	selected := f.submit(t, jobSubmission("selected", "shared"))
	ret, err := f.service.ClaimByID(t.Context(), selected.UUID, selected.Revision, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.Nil(t, ret, "a claim by ID cannot bypass another kind's resource lock")
	require.Equal(t, selected, f.find(t, selected.UUID))
	f.outcome(t, blocker, models.ArchiveJobOutcome{State: "succeeded", Result: json.RawMessage(`{}`)})
	claimed, err := f.service.ClaimByID(t.Context(), selected.UUID, selected.Revision, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	progress, err := f.service.Progress(t.Context(), claimed.Lease(), json.RawMessage(`{"checkpoint":"retained"}`))
	require.NoError(t, err)
	retryAt := f.now.Add(time.Hour)
	retry := f.outcome(t, claimed, models.ArchiveJobOutcome{State: "retry", RetryAt: retryAt, ErrorCode: "source_cooldown", Result: json.RawMessage(`{}`)})
	ret, err = f.service.ClaimByID(t.Context(), retry.UUID, retry.Revision, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.Nil(t, ret, "an explicit claim must preserve retry backoff")
	require.Equal(t, retry, f.find(t, retry.UUID))
	f.now = retryAt
	resumed, err := f.service.ClaimByID(t.Context(), retry.UUID, retry.Revision, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, resumed)
	require.EqualValues(t, 2, resumed.Fence)
	require.Equal(t, progress.Progress, resumed.Progress)
	completed := f.outcome(t, resumed, models.ArchiveJobOutcome{State: "succeeded", Result: json.RawMessage(`{}`)})
	ret, err = f.service.ClaimByID(t.Context(), completed.UUID, completed.Revision, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.Nil(t, ret, "an explicit claim must not reopen completed work")
}

func TestArchiveJobSelectedClaimRejectsStaleSelectionAndInvalidInput(t *testing.T) {
	f := newDurableJobFixture(t)
	input := jobSubmission("selected", "resource")
	selected := f.submit(t, input)
	input.RequestUUID = uuid.NewString()
	input.Priority++
	promoted := f.submit(t, input)
	for _, stale := range []struct {
		id       string
		revision int64
	}{{selected.UUID, selected.Revision}, {uuid.NewString(), 1}} {
		ret, err := f.service.ClaimByID(t.Context(), stale.id, stale.revision, uuid.NewString(), time.Minute)
		require.ErrorIs(t, err, models.ErrArchiveJobConflict)
		require.Nil(t, ret)
	}
	for _, invalid := range []struct {
		id       string
		revision int64
		owner    string
		duration time.Duration
	}{
		{"invalid", promoted.Revision, uuid.NewString(), time.Minute},
		{"00000000-0000-4000-8000-00000000000A", promoted.Revision, uuid.NewString(), time.Minute},
		{promoted.UUID, 0, uuid.NewString(), time.Minute},
		{promoted.UUID, promoted.Revision, "invalid", time.Minute},
		{promoted.UUID, promoted.Revision, uuid.Nil.String(), time.Minute},
		{promoted.UUID, promoted.Revision, uuid.NewString(), time.Second},
	} {
		ret, err := f.service.ClaimByID(t.Context(), invalid.id, invalid.revision, invalid.owner, invalid.duration)
		require.Error(t, err)
		require.Nil(t, ret)
	}
	ctx, err := f.db.Begin(t.Context(), true)
	require.NoError(t, err)
	_, err = f.repo.ArchiveJob.ClaimByID(ctx, promoted.UUID, promoted.Revision, uuid.NewString(), f.now, time.Minute)
	require.ErrorContains(t, err, "managed write transaction")
	require.NoError(t, f.db.Rollback(ctx))
	require.Equal(t, promoted, f.find(t, promoted.UUID))
}

func TestArchiveJobSelectedClaimHasOneConcurrentOwner(t *testing.T) {
	f := newDurableJobFixture(t)
	selected := f.submit(t, jobSubmission("selected", "resource"))
	var wg sync.WaitGroup
	type result struct {
		job *models.ArchiveJob
		err error
	}
	results := make(chan result, 8)
	for range 8 {
		wg.Go(func() {
			ret, err := f.service.ClaimByID(t.Context(), selected.UUID, selected.Revision, uuid.NewString(), time.Minute)
			results <- result{ret, err}
		})
	}
	wg.Wait()
	close(results)
	claimed := 0
	for result := range results {
		if result.err != nil {
			require.ErrorIs(t, result.err, models.ErrArchiveJobConflict)
			require.Nil(t, result.job)
			continue
		}
		require.NotNil(t, result.job)
		require.Equal(t, selected.UUID, result.job.UUID)
		claimed++
	}
	require.Equal(t, 1, claimed)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		attempts, err := f.repo.ArchiveJob.Attempts(ctx, selected.UUID, 0, 10)
		require.NoError(t, err)
		require.Len(t, attempts, 1)
		return nil
	}))
}

func TestArchiveJobClaimsRollbackSwallowedAttemptFailure(t *testing.T) {
	for _, byID := range []bool{false, true} {
		name := "queue"
		if byID {
			name = "selected"
		}
		t.Run(name, func(t *testing.T) {
			f := newDurableJobFixture(t)
			selected := f.submit(t, jobSubmission("selected", "resource"))
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			_, err := raw.Exec(`CREATE TRIGGER archive_claim_test_failure BEFORE INSERT ON archive_job_attempts
BEGIN SELECT RAISE(FAIL, 'injected attempt failure'); END`)
			require.NoError(t, err)
			err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.Ingest.CreateProducer(ctx, "Must roll back")
				require.NoError(t, err)
				if byID {
					_, err = f.repo.ArchiveJob.ClaimByID(ctx, selected.UUID, selected.Revision, uuid.NewString(), f.now, time.Minute)
				} else {
					_, err = f.repo.ArchiveJob.Claim(ctx, selected.Kind, uuid.NewString(), f.now, time.Minute)
				}
				require.ErrorContains(t, err, "injected attempt failure")
				return nil // A caller cannot turn an incomplete claim into a commit.
			})
			require.ErrorIs(t, err, models.ErrArchiveJobConflict)
			require.Equal(t, selected, f.find(t, selected.UUID))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_job_attempts"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM ingest_producers"))
			_, err = raw.Exec("DROP TRIGGER archive_claim_test_failure")
			require.NoError(t, err)
		})
	}
}

func TestArchiveJobsCancellationInvalidatesLeaseAndUsesReviewedRevision(t *testing.T) {
	f := newDurableJobFixture(t)
	queued := f.submit(t, jobSubmission("cancel", "destination"))
	running := f.claim(t, uuid.NewString())
	_, err := f.service.Cancel(t.Context(), queued.UUID, queued.Revision)
	require.ErrorIs(t, err, models.ErrArchiveJobConflict)
	cancelled, err := f.service.Cancel(t.Context(), running.UUID, running.Revision)
	require.NoError(t, err)
	require.Equal(t, "cancelled", cancelled.State)
	_, err = f.service.Renew(t.Context(), running.Lease(), time.Minute)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	_, err = f.service.Cancel(t.Context(), cancelled.UUID, cancelled.Revision)
	require.NoError(t, err)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	for _, query := range []string{
		"UPDATE archive_jobs SET state='queued',revision=revision+1",
		"UPDATE archive_jobs SET arguments='{}',revision=revision+1",
		"UPDATE archive_jobs SET revision=revision-1",
		"UPDATE archive_job_submissions SET digest=printf('%064d',0)",
		"UPDATE archive_job_attempts SET outcome='succeeded'",
	} {
		_, err := raw.Exec(query)
		require.Error(t, err, query)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestArchiveJobsConcurrentSubmissionsAndClaims(t *testing.T) {
	f := newDurableJobFixture(t)
	var wg sync.WaitGroup
	type result struct {
		job *models.ArchiveJob
		err error
	}
	results := make(chan result, 8)
	for range 8 {
		wg.Go(func() {
			job, err := f.service.Submit(t.Context(), jobSubmission("same", "same"))
			results <- result{job, err}
		})
	}
	wg.Wait()
	close(results)
	var id string
	for ret := range results {
		require.NoError(t, ret.err)
		if id == "" {
			id = ret.job.UUID
		}
		require.Equal(t, id, ret.job.UUID)
	}
	results = make(chan result, 8)
	for range 8 {
		wg.Go(func() {
			job, err := f.service.Claim(t.Context(), models.ArchiveJobVerifyMedia, uuid.NewString(), time.Minute)
			results <- result{job, err}
		})
	}
	wg.Wait()
	close(results)
	count := 0
	for ret := range results {
		require.NoError(t, ret.err)
		if ret.job != nil {
			count++
		}
	}
	require.Equal(t, 1, count)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM archive_jobs"))
	require.EqualValues(t, 8, queryUint(t, raw, "SELECT count(*) FROM archive_job_submissions"))
}

func TestArchiveJobPublicationIsAtomicAndRejectsExpiryBeforeCommit(t *testing.T) {
	for _, mode := range []string{"success", "domain failure", "expired during apply", "expired before commit"} {
		t.Run(mode, func(t *testing.T) {
			f := newDurableJobFixture(t)
			f.submit(t, jobSubmission("publish", "destination"))
			claimed := f.claim(t, uuid.NewString())
			published, err := f.service.Publish(t.Context(), claimed.Lease(), func(ctx context.Context, current *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
				require.Equal(t, claimed, current)
				if _, err := f.repo.Ingest.CreateProducer(ctx, "Atomic domain fixture"); err != nil {
					return models.ArchiveJobOutcome{}, err
				}
				switch mode {
				case "domain failure":
					return models.ArchiveJobOutcome{}, errors.New("fixture domain failure")
				case "expired during apply":
					f.now = *claimed.LeaseUntil
				case "expired before commit":
					txn.AddPreCommitHook(ctx, func(context.Context) error { f.now = *claimed.LeaseUntil; return nil })
				}
				return models.ArchiveJobOutcome{State: "succeeded", Result: json.RawMessage(`{"committed":true}`)}, nil
			})
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			if mode == "success" {
				require.NoError(t, err)
				require.Equal(t, "succeeded", published.State)
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM ingest_producers"))
			} else {
				require.Error(t, err)
				require.Nil(t, published)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM ingest_producers"))
				require.Equal(t, claimed, f.find(t, claimed.UUID), "domain failure must not report a successful attempt")
			}
		})
	}
}

func TestArchiveJobCheckpointKeepsDomainAndProgressAtomic(t *testing.T) {
	for _, mode := range []string{"success", "domain failure", "expired before commit"} {
		t.Run(mode, func(t *testing.T) {
			f := newDurableJobFixture(t)
			f.submit(t, jobSubmission("checkpoint", "destination"))
			claimed := f.claim(t, uuid.NewString())
			checkpoint, err := f.service.Checkpoint(t.Context(), claimed.Lease(), func(ctx context.Context, _ *models.ArchiveJob) (json.RawMessage, error) {
				if _, err := f.repo.Ingest.CreateProducer(ctx, "Checkpoint fixture"); err != nil {
					return nil, err
				}
				if mode == "domain failure" {
					return nil, errors.New("fixture failure")
				}
				if mode == "expired before commit" {
					txn.AddPreCommitHook(ctx, func(context.Context) error { f.now = *claimed.LeaseUntil; return nil })
				}
				return json.RawMessage(`{"phase":"published"}`), nil
			})
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			if mode != "success" {
				require.Error(t, err)
				require.Nil(t, checkpoint)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM ingest_producers"))
				require.Equal(t, claimed, f.find(t, claimed.UUID))
				return
			}
			require.NoError(t, err)
			require.Equal(t, "running", checkpoint.State)
			require.JSONEq(t, `{"phase":"published"}`, string(checkpoint.Progress))
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM ingest_producers"))
			f.now = *checkpoint.LeaseUntil
			resumed := f.claim(t, uuid.NewString())
			require.NotNil(t, resumed)
			require.Equal(t, checkpoint.Progress, resumed.Progress)
			require.EqualValues(t, 2, resumed.Fence)
		})
	}
}

func TestArchiveJobsValidationIndexesAndAnonymisation(t *testing.T) {
	f := newDurableJobFixture(t)
	input := jobSubmission("validation", "resource")
	ctx, err := f.db.Begin(t.Context(), true)
	require.NoError(t, err)
	_, err = f.repo.ArchiveJob.Submit(ctx, input, f.now, 10)
	require.ErrorContains(t, err, "managed write transaction")
	require.NoError(t, f.db.Rollback(ctx))
	for _, args := range []json.RawMessage{[]byte(`{"duplicate":1,"duplicate":2}`), []byte(`[]`), []byte(`{"large":"` + strings.Repeat("x", 262144) + `"}`)} {
		bad := input
		bad.Arguments = args
		_, err := f.service.Submit(t.Context(), bad)
		require.Error(t, err)
	}
	bad := input
	bad.Kind = "arbitrary.shell"
	_, err = f.service.Submit(t.Context(), bad)
	require.Error(t, err)
	queued := f.submit(t, input)
	_, err = f.service.Claim(t.Context(), input.Kind, uuid.NewString(), time.Second)
	require.Error(t, err)
	claimed := f.claim(t, uuid.NewString())
	_, err = f.service.Publish(t.Context(), claimed.Lease(), func(context.Context, *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		return models.ArchiveJobOutcome{State: "failed", Result: json.RawMessage(`{}`), ErrorCode: "raw worker stderr\nnot a code"}, nil
	})
	require.Error(t, err)
	require.Equal(t, claimed, f.find(t, queued.UUID))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	for query, index := range map[string]string{
		"SELECT * FROM archive_jobs WHERE kind='media.verify' AND state='queued' AND id>0 ORDER BY id LIMIT 10":       "archive_jobs_list",
		"SELECT * FROM archive_jobs WHERE state='running' AND lease_until_ms<=10 ORDER BY lease_until_ms,id LIMIT 10": "archive_jobs_expired",
		"SELECT * FROM archive_jobs WHERE state='running' AND resource_key='fixture'":                                 "archive_jobs_running_resource",
		`SELECT j.* FROM archive_jobs j WHERE j.uuid='fixture' AND j.state='queued' AND j.available_at_ms<=10 AND j.fence<j.max_attempts
 AND NOT EXISTS(SELECT 1 FROM archive_jobs r WHERE r.state='running' AND r.resource_key=j.resource_key)`: "SEARCH j USING INDEX sqlite_autoindex_archive_jobs_",
	} {
		rows, err := raw.Query("EXPLAIN QUERY PLAN " + query)
		require.NoError(t, err)
		defer rows.Close()
		var plan string
		for rows.Next() {
			var a, b, c int
			var detail string
			require.NoError(t, rows.Scan(&a, &b, &c, &detail))
			plan += detail
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		require.Contains(t, plan, index)
	}
	out := filepath.Join(t.TempDir(), "anonymous-jobs.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, out)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	anon := openRawDB(t, out)
	defer anon.Close()
	for _, table := range []string{"archive_jobs", "archive_job_submissions", "archive_job_attempts"} {
		require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestArchiveJobMigrationAndStartupGuards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema18.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+18; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	id := uuid.NewString()
	_, err = raw.Exec("INSERT INTO ingest_producers(uuid,label) VALUES(?,?)", id, "Retained producer")
	require.NoError(t, err)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	var label string
	require.NoError(t, raw.QueryRow("SELECT label FROM ingest_producers WHERE uuid=?", id).Scan(&label))
	require.Equal(t, "Retained producer", label)
	for _, table := range []string{"archive_jobs", "archive_job_submissions", "archive_job_attempts"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, raw.Close())
	for _, name := range []string{"archive_jobs_running_resource", "archive_job_transition", "archive_job_attempt_valid"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "incomplete.sqlite")
			writeEmptyNativeFixture(t, path)
			raw := openRawDB(t, path)
			kind := "TRIGGER"
			if name == "archive_jobs_running_resource" {
				kind = "INDEX"
			}
			_, err := raw.Exec("DROP " + kind + " " + name)
			require.NoError(t, err)
			require.NoError(t, raw.Close())
			db := sqlite.NewDatabase()
			require.ErrorContains(t, db.Open(path), "missing "+name)
		})
	}
}
