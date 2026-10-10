package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/translation"
	"github.com/stretchr/testify/require"
)

func translationService(t *testing.T, repo models.Repository) (*translation.Service, *atomic.Int64) {
	t.Helper()
	clock := &atomic.Int64{}
	clock.Store(time.Now().UnixMilli())
	s := translation.New(repo)
	s.Durable.Now = func() time.Time { return time.UnixMilli(clock.Load()).UTC() }
	return s, clock
}

func translationJobTarget(t *testing.T, repo models.Repository, request, key string, schedule models.TranslationTargetSchedule, now time.Time) *models.TranslationTarget {
	t.Helper()
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: key}, "")
	return retainTranslationTarget(t, repo, models.TranslationTargetInput{RequestUUID: request, PostUUID: post.UUID, Field: "caption", Origin: "capture"}, schedule, now)
}

func translationTarget(t *testing.T, repo models.Repository, id string) *models.TranslationTarget {
	t.Helper()
	var ret *models.TranslationTarget
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.TranslationWork.Target(ctx, id)
		return err
	}))
	return ret
}

func translationStatus(t *testing.T, s *translation.Service, id string) *models.ArchiveJob {
	t.Helper()
	ret, err := s.Status(t.Context(), id)
	require.NoError(t, err)
	return ret
}

func translationOutput() models.TranslationCacheInput {
	return models.TranslationCacheInput{Status: "translated", TranslatedText: translationPointer("Translated source text"), SourceLanguage: translationPointer("ja"), Provider: translationPointer("fixture")}
}

func processTranslation(t *testing.T, worker *translation.Worker) {
	t.Helper()
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
}

func TestTranslationJobsBoundedAdmissionSharedCacheAndHolds(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	s, clock := translationService(t, repo)
	request := retainTranslationRequest(t, repo, "Shared original")
	ready := models.TranslationTargetSchedule{State: "pending", Priority: 25}
	for i := range 53 {
		translationJobTarget(t, repo, request.UUID, fmt.Sprintf("shared-%d", i), ready, s.Durable.Now())
	}
	held := translationJobTarget(t, repo, request.UUID, "held", models.TranslationTargetSchedule{State: "held", Priority: 100}, s.Durable.Now())
	later := translationJobTarget(t, repo, request.UUID, "later", models.TranslationTargetSchedule{State: "pending", NotBefore: s.Durable.Now().Add(time.Hour)}, s.Durable.Now())
	first, err := s.Admit(t.Context())
	require.NoError(t, err)
	require.NotNil(t, first)
	work, err := archive.DecodeTranslationJob(first)
	require.NoError(t, err)
	require.Len(t, work.Targets, 50)
	require.Equal(t, 25, first.Priority)
	again, err := s.Admit(t.Context())
	require.NoError(t, err)
	require.Nil(t, again, "one active batch owns the shared request")
	calls := 0
	worker := translation.NewWorker(s, translation.ProviderFunc(func(_ context.Context, input models.TranslationRequest) (models.TranslationCacheInput, error) {
		calls++
		require.Equal(t, request.UUID, input.UUID)
		return translationOutput(), nil
	}))
	processTranslation(t, worker)
	require.Equal(t, "succeeded", translationStatus(t, s, first.UUID).State)
	processTranslation(t, worker)
	require.Equal(t, 1, calls, "later batches reuse the same provider result")
	require.Equal(t, held, translationTarget(t, repo, held.UUID))
	require.Equal(t, later, translationTarget(t, repo, later.UUID))
	clock.Add(time.Hour.Milliseconds())
	processTranslation(t, worker)
	require.Equal(t, "completed", translationTarget(t, repo, later.UUID).State)
	require.Equal(t, "held", translationTarget(t, repo, held.UUID).State)
	require.Equal(t, 1, calls)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM archive_jobs WHERE kind='text.translate'"))
	require.EqualValues(t, 54, queryUint(t, raw, "SELECT count(*) FROM translation_job_targets"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM translation_cache"))
	require.EqualValues(t, 54, queryUint(t, raw, "SELECT count(*) FROM source_translation_evidence"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()), "job bindings and target history validate on restart")
	anonPath := filepath.Join(t.TempDir(), "translation-jobs-anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(db, anonPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	anon := openRawDB(t, anonPath)
	defer anon.Close()
	for _, table := range []string{"translation_job_targets", "archive_jobs", "translation_targets", "translation_requests", "translation_cache"} {
		require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestTranslationJobsPriorityCapacityAndConcurrentAdmission(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	s, _ := translationService(t, repo)
	s.MaxActive = 1
	low := retainTranslationRequest(t, repo, "Historical input")
	high := retainTranslationRequest(t, repo, "New capture input")
	translationJobTarget(t, repo, low.UUID, "low", models.TranslationTargetSchedule{State: "pending"}, s.Durable.Now())
	translationJobTarget(t, repo, high.UUID, "high", models.TranslationTargetSchedule{State: "pending", Priority: 100}, s.Durable.Now())
	type admission struct {
		job *models.ArchiveJob
		err error
	}
	results := make(chan admission, 2)
	for range 2 {
		go func() { result, err := s.Admit(t.Context()); results <- admission{result, err} }()
	}
	count := 0
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		if result.job != nil {
			count++
			work, err := archive.DecodeTranslationJob(result.job)
			require.NoError(t, err)
			require.Equal(t, high.UUID, work.RequestUUID)
		}
	}
	require.Equal(t, 1, count)
}

func TestTranslationJobsRetryDelayCannotBeBypassedByNewTargets(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	s, clock := translationService(t, repo)
	request := retainTranslationRequest(t, repo, "Retry source")
	first := translationJobTarget(t, repo, request.UUID, "retry-first", models.TranslationTargetSchedule{State: "pending"}, s.Durable.Now())
	admitted, err := s.Admit(t.Context())
	require.NoError(t, err)
	calls := 0
	worker := translation.NewWorker(s, translation.ProviderFunc(func(context.Context, models.TranslationRequest) (models.TranslationCacheInput, error) {
		calls++
		if calls == 1 {
			return models.TranslationCacheInput{}, errors.New("private provider stderr and original text")
		}
		return translationOutput(), nil
	}))
	processTranslation(t, worker)
	failed := translationStatus(t, s, admitted.UUID)
	require.Equal(t, "queued", failed.State)
	require.Equal(t, "translation_unavailable", failed.ErrorCode)
	require.Equal(t, s.Durable.Now().Add(5*time.Minute), failed.AvailableAt)
	late := translationJobTarget(t, repo, request.UUID, "retry-later", models.TranslationTargetSchedule{State: "pending", Priority: 100}, s.Durable.Now())
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.False(t, processed)
	require.Equal(t, 1, calls)
	clock.Add((5 * time.Minute).Milliseconds())
	processTranslation(t, worker)
	require.Equal(t, "completed", translationTarget(t, repo, first.UUID).State)
	require.Equal(t, "pending", translationTarget(t, repo, late.UUID).State, "an admitted batch cannot acquire a later target")
	processTranslation(t, worker)
	require.Equal(t, "completed", translationTarget(t, repo, late.UUID).State)
	require.Equal(t, 2, calls)
}

func TestTranslationJobsCancellationFencesProviderAndExplicitRetry(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	s, _ := translationService(t, repo)
	request := retainTranslationRequest(t, repo, "Cancel source")
	target := translationJobTarget(t, repo, request.UUID, "cancelled", models.TranslationTargetSchedule{State: "pending", Priority: 25}, s.Durable.Now())
	admitted, err := s.Admit(t.Context())
	require.NoError(t, err)
	worker := translation.NewWorker(s, translation.ProviderFunc(func(context.Context, models.TranslationRequest) (models.TranslationCacheInput, error) {
		current := translationStatus(t, s, admitted.UUID)
		_, err := s.Cancel(t.Context(), current.UUID, current.Revision)
		require.NoError(t, err)
		return translationOutput(), nil
	}))
	processed, err := worker.ProcessNext(t.Context())
	require.True(t, processed)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM translation_cache"))
	held := translationTarget(t, repo, target.UUID)
	require.Equal(t, "held", held.State)
	require.Equal(t, target.Revision+1, held.Revision)
	require.Equal(t, target.NotBefore, held.NotBefore)
	next, err := s.Admit(t.Context())
	require.NoError(t, err)
	require.Nil(t, next)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationWork.RetryTarget(ctx, held.UUID, held.Revision, s.Durable.Now())
		return err
	}))
	worker.Provider = translation.ProviderFunc(func(context.Context, models.TranslationRequest) (models.TranslationCacheInput, error) {
		return translationOutput(), nil
	})
	processTranslation(t, worker)
	require.Equal(t, "completed", translationTarget(t, repo, target.UUID).State)
	require.Equal(t, "cancelled", translationStatus(t, s, admitted.UUID).State)
}

func TestTranslationJobsChangedTargetsCannotBePublishedByOldWorker(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	s, _ := translationService(t, repo)
	request := retainTranslationRequest(t, repo, "Held while running")
	target := translationJobTarget(t, repo, request.UUID, "hold-running", models.TranslationTargetSchedule{State: "pending"}, s.Durable.Now())
	worker := translation.NewWorker(s, translation.ProviderFunc(func(context.Context, models.TranslationRequest) (models.TranslationCacheInput, error) {
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.TranslationWork.ScheduleTarget(ctx, target.UUID, target.Revision, models.TranslationTargetSchedule{State: "held", NotBefore: target.NotBefore}, s.Durable.Now())
			return err
		}))
		return translationOutput(), nil
	}))
	processTranslation(t, worker)
	held := translationTarget(t, repo, target.UUID)
	require.Equal(t, "held", held.State)
	require.Nil(t, held.EvidenceUUID)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		cache, err := repo.TranslationWork.Cache(ctx, request.UUID)
		require.NoError(t, err)
		require.NotNil(t, cache, "the shared result remains useful without releasing the held target")
		return nil
	}))
}

func TestTranslationJobsCacheCheckpointSurvivesPublicationFailureAndRestore(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	s, clock := translationService(t, repo)
	request := retainTranslationRequest(t, repo, "Checkpoint input")
	target := translationJobTarget(t, repo, request.UUID, "checkpoint", models.TranslationTargetSchedule{State: "pending"}, s.Durable.Now())
	admitted, err := s.Admit(t.Context())
	require.NoError(t, err)
	attachmentSQL(t, db, "CREATE TRIGGER reject_translation_publish BEFORE UPDATE ON translation_targets WHEN NEW.state='completed' BEGIN SELECT RAISE(ABORT,'fixture failure'); END")
	calls := 0
	provider := translation.ProviderFunc(func(context.Context, models.TranslationRequest) (models.TranslationCacheInput, error) {
		calls++
		return translationOutput(), nil
	})
	processTranslation(t, translation.NewWorker(s, provider))
	require.Equal(t, "queued", translationStatus(t, s, admitted.UUID).State)
	require.Equal(t, target, translationTarget(t, repo, target.UUID), "partial target/evidence publication rolls back")
	attachmentSQL(t, db, "DROP TRIGGER reject_translation_publish")
	backup := filepath.Join(t.TempDir(), "translation-execution.sqlite")
	require.NoError(t, db.Backup(backup))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(backup))
	repo = db.Repository()
	clock.Add((5 * time.Minute).Milliseconds())
	s.Durable.Repo = repo
	processTranslation(t, translation.NewWorker(s, provider))
	require.Equal(t, 1, calls, "a new worker reuses the checkpointed provider result")
	require.Equal(t, "completed", translationTarget(t, repo, target.UUID).State)
	require.Equal(t, "succeeded", translationStatus(t, s, admitted.UUID).State)
}

func TestTranslationJobsForgottenPostsAvoidProviderAndRetainReview(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	s, _ := translationService(t, repo)
	request := retainTranslationRequest(t, repo, "Forgotten post")
	target := translationJobTarget(t, repo, request.UUID, "forgotten-work", models.TranslationTargetSchedule{State: "pending"}, s.Durable.Now())
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", target.PostUUID)
	require.NoError(t, err)
	worker := translation.NewWorker(s, translation.ProviderFunc(func(context.Context, models.TranslationRequest) (models.TranslationCacheInput, error) {
		t.Error("forgotten posts do not need a network request")
		return models.TranslationCacheInput{}, translation.ErrProviderFailure
	}))
	processTranslation(t, worker)
	review := translationTarget(t, repo, target.UUID)
	require.Equal(t, "review", review.State)
	require.Equal(t, "post_forgotten", review.Reason)
	require.Nil(t, review.CacheUUID)
	require.Nil(t, review.EvidenceUUID)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestTranslationJobsExpiredLeasesResumeWithoutExplicitRetry(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	s, clock := translationService(t, repo)
	request := retainTranslationRequest(t, repo, "Expired work")
	target := translationJobTarget(t, repo, request.UUID, "expired-work", models.TranslationTargetSchedule{State: "pending"}, s.Durable.Now())
	submission, err := archive.PrepareTranslationJob(models.TranslationJobArguments{Version: 1, RequestUUID: request.UUID, Targets: []models.TranslationTargetRef{{TargetUUID: target.UUID, Revision: target.Revision}}})
	require.NoError(t, err)
	submission.MaxAttempts = 1
	var current *models.ArchiveJob
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		current, err = repo.ArchiveJob.Submit(ctx, submission, s.Durable.Now(), 100)
		if err != nil {
			return err
		}
		return repo.TranslationWork.BindJob(ctx, current.UUID, s.Durable.Now())
	}))
	claimed, err := s.Durable.Claim(t.Context(), models.ArchiveJobTranslateText, uuid.NewString(), 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, current.UUID, claimed.UUID)
	clock.Add((6 * time.Second).Milliseconds())
	count, err := s.Durable.Recover(t.Context(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, "queued", translationStatus(t, s, current.UUID).State)
	resumed, err := s.Durable.Claim(t.Context(), models.ArchiveJobTranslateText, uuid.NewString(), 5*time.Second)
	require.NoError(t, err)
	require.NotNil(t, resumed)
	require.Equal(t, current.UUID, resumed.UUID)
	require.EqualValues(t, 2, resumed.Fence)
	require.Zero(t, resumed.Failures)
	require.Equal(t, target.Revision, translationTarget(t, repo, target.UUID).Revision)
}

func TestTranslationJobsAdmissionRollsBackMissingOrInvalidBindings(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	s, _ := translationService(t, repo)
	request := retainTranslationRequest(t, repo, "Unbound request")
	target := translationJobTarget(t, repo, request.UUID, "unbound", models.TranslationTargetSchedule{State: "held"}, s.Durable.Now())
	submission, err := archive.PrepareTranslationJob(models.TranslationJobArguments{Version: 1, RequestUUID: request.UUID, Targets: []models.TranslationTargetRef{{TargetUUID: target.UUID, Revision: target.Revision}}})
	require.NoError(t, err)
	for _, bind := range []bool{false, true} {
		err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
			current, err := repo.ArchiveJob.Submit(ctx, submission, s.Durable.Now(), 100)
			require.NoError(t, err)
			if bind {
				require.ErrorIs(t, repo.TranslationWork.BindJob(ctx, current.UUID, s.Durable.Now()), models.ErrTranslationWorkConflict)
			}
			return nil
		})
		require.Error(t, err, "even a caller swallowing binding errors cannot commit orphan work")
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_job_submissions"))
}

func TestTranslationJobsAuditRefusesLostBindingWithoutWrites(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	s, _ := translationService(t, repo)
	request := retainTranslationRequest(t, repo, "Corruption fixture")
	translationJobTarget(t, repo, request.UUID, "corrupt-binding", models.TranslationTargetSchedule{State: "pending"}, s.Durable.Now())
	_, err := s.Admit(t.Context())
	require.NoError(t, err)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	_, err = raw.Exec("DELETE FROM translation_job_targets")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	check := sqlite.NewDatabase()
	require.ErrorContains(t, check.AuditForTesting(path), "translation job target bindings")
	require.NoError(t, check.Close())
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestTranslationJobsPermanentProviderFailureHoldsWork(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	s, _ := translationService(t, repo)
	request := retainTranslationRequest(t, repo, "Unsupported input")
	target := translationJobTarget(t, repo, request.UUID, "permanent", models.TranslationTargetSchedule{State: "pending"}, s.Durable.Now())
	current, err := s.Admit(t.Context())
	require.NoError(t, err)
	worker := translation.NewWorker(s, translation.ProviderFunc(func(context.Context, models.TranslationRequest) (models.TranslationCacheInput, error) {
		return models.TranslationCacheInput{}, translation.ErrProviderInput
	}))
	processTranslation(t, worker)
	failed := translationStatus(t, s, current.UUID)
	require.Equal(t, "failed", failed.State)
	require.Equal(t, "translation_input_unsupported", failed.ErrorCode)
	require.Equal(t, "held", translationTarget(t, repo, target.UUID).State)
	var result map[string]any
	require.NoError(t, json.Unmarshal(failed.Result, &result))
	require.Empty(t, result)
}

func TestTranslationJobsLateProviderCannotCacheAfterLeaseExpiry(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	s, clock := translationService(t, repo)
	request := retainTranslationRequest(t, repo, "Late provider response")
	target := translationJobTarget(t, repo, request.UUID, "late-provider", models.TranslationTargetSchedule{State: "pending"}, s.Durable.Now())
	calls := 0
	worker := translation.NewWorker(s, translation.ProviderFunc(func(context.Context, models.TranslationRequest) (models.TranslationCacheInput, error) {
		calls++
		if calls == 1 {
			clock.Add((2 * time.Minute).Milliseconds())
		}
		return translationOutput(), nil
	}))
	processed, err := worker.ProcessNext(t.Context())
	require.True(t, processed)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM translation_cache"))
	require.Equal(t, target, translationTarget(t, repo, target.UUID))
	processTranslation(t, worker)
	require.Equal(t, "completed", translationTarget(t, repo, target.UUID).State)
	require.Equal(t, 2, calls)
}
