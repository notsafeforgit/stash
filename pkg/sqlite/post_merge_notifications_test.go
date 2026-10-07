package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func postMergeNotificationFixture(t *testing.T) (*Database, models.Repository, *models.PostConsolidationReview) {
	t.Helper()
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "native:reddit", "one-post")
	capture := postSelectionCapture(t, repo, a, 0, 1)
	postSelectionApply(t, repo, a, capture.UUID, "pinned", "review")
	input := consolidationReviewRequest(t, repo, models.PostConsolidationReviewInput{SourceUUID: a, DestinationUUID: b})
	review, _ := applyConsolidationReview(t, repo, input)
	require.NotEmpty(t, review.Result.NotificationJobUUID)
	return db, repo, review
}

var postMergeNotificationDomainTables = []string{"source_post_identities", "source_post_consolidations", "source_captures", "post_attachment_decisions",
	"post_gallery_decisions", "post_media_decisions", "attachment_media_decisions", "galleries", "gallery_membership_events", "post_consolidation_reviews"}

func TestPostMergeNotificationsRecoverOriginalResultAfterRestartAndLaterMerge(t *testing.T) {
	db, repo, review := postMergeNotificationFixture(t)
	now := time.Now().UTC()
	service := gallery.NewPostMergeNotifications(repo)
	service.Durable.Now = func() time.Time { return now }
	var seen []models.PostConsolidationReview
	worker := gallery.NewPostMergeNotificationWorker(service, func(ctx context.Context, stored models.PostConsolidationReview, guard gallery.AlbumEffectGuard) error {
		require.NoError(t, guard(ctx))
		seen = append(seen, stored)
		return errors.New("plugin temporarily unavailable")
	})
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	current, err := service.Find(t.Context(), review.Result.NotificationJobUUID)
	require.NoError(t, err)
	require.Equal(t, "queued", current.State)
	require.Equal(t, "post_merge_notification_unavailable", current.ErrorCode)
	processed, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.False(t, processed, "honor retry backoff")
	c := identityPost(t, repo, "legacy:catalog:fixture", "later-survivor")
	applyConsolidationReview(t, repo, consolidationReviewRequest(t, repo, models.PostConsolidationReviewInput{
		SourceUUID: review.Request.DestinationUUID, DestinationUUID: c}))
	before := identityRows(t, repo, postMergeNotificationDomainTables...)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	service = gallery.NewPostMergeNotifications(repo)
	now = now.Add(time.Minute)
	service.Durable.Now = func() time.Time { return now }
	worker = gallery.NewPostMergeNotificationWorker(service, func(ctx context.Context, stored models.PostConsolidationReview, guard gallery.AlbumEffectGuard) error {
		require.NoError(t, guard(ctx))
		seen = append(seen, stored)
		return nil
	})
	processed, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, []models.PostConsolidationReview{*review, *review}, seen)
	current, err = service.Find(t.Context(), review.Result.NotificationJobUUID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", current.State)
	require.JSONEq(t, `{"hooks_finished":true}`, string(current.Result))
	require.Equal(t, before, identityRows(t, repo, postMergeNotificationDomainTables...))
}

func TestPostMergeNotificationsCancelFenceAndExplicitRetryBindSavedRequest(t *testing.T) {
	db, repo, review := postMergeNotificationFixture(t)
	service := gallery.NewPostMergeNotifications(repo)
	worker := gallery.NewPostMergeNotificationWorker(service, func(ctx context.Context, _ models.PostConsolidationReview, guard gallery.AlbumEffectGuard) error {
		current, err := service.Find(ctx, review.Result.NotificationJobUUID)
		require.NoError(t, err)
		cancelled, err := service.Cancel(ctx, current.UUID, current.Revision)
		require.NoError(t, err)
		replayed, err := service.Cancel(ctx, current.UUID, current.Revision)
		require.NoError(t, err)
		require.Equal(t, cancelled, replayed)
		return guard(ctx)
	})
	processed, err := worker.ProcessNext(t.Context())
	require.True(t, processed)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	parent, err := service.Find(t.Context(), review.Result.NotificationJobUUID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", parent.State)
	before := identityRows(t, repo, postMergeNotificationDomainTables...)
	request := uuid.NewString()
	child, err := service.Retry(t.Context(), request, parent.UUID, parent.Revision)
	require.NoError(t, err)
	require.NotEqual(t, parent.UUID, child.UUID)
	_, err = service.Retry(t.Context(), request, parent.UUID, parent.Revision+1)
	require.ErrorIs(t, err, models.ErrArchiveJobConflict, "same request with a changed parent revision must not recover")
	_, err = service.Retry(t.Context(), request, child.UUID, child.Revision)
	require.ErrorIs(t, err, models.ErrArchiveJobConflict, "same request cannot change the parent job")
	_, err = service.Retry(t.Context(), uuid.NewString(), child.UUID, child.Revision)
	require.ErrorIs(t, err, models.ErrArchiveJobConflict, "an active delivery cannot be retried explicitly")
	worker = gallery.NewPostMergeNotificationWorker(service, func(ctx context.Context, stored models.PostConsolidationReview, guard gallery.AlbumEffectGuard) error {
		require.Equal(t, *review, stored)
		return guard(ctx)
	})
	processed, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	service = gallery.NewPostMergeNotifications(repo)
	recovered, err := service.Retry(t.Context(), request, parent.UUID, parent.Revision)
	require.NoError(t, err)
	require.Equal(t, child.UUID, recovered.UUID)
	require.Equal(t, "succeeded", recovered.State)
	require.Equal(t, before, identityRows(t, repo, postMergeNotificationDomainTables...))
}

func TestPostMergeNotificationsRecoverExpiredLeaseAfterEffectBeforeAcknowledgement(t *testing.T) {
	db, repo, review := postMergeNotificationFixture(t)
	now := time.Now().UTC()
	service := gallery.NewPostMergeNotifications(repo)
	service.Durable.Now = func() time.Time { return now }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	seen := []string{}
	worker := gallery.NewPostMergeNotificationWorker(service, func(ctx context.Context, stored models.PostConsolidationReview, guard gallery.AlbumEffectGuard) error {
		require.NoError(t, guard(ctx))
		seen = append(seen, stored.Request.RequestUUID)
		cancel() // Effect completed, process exited before recording delivery.
		return ctx.Err()
	})
	processed, err := worker.ProcessNext(ctx)
	require.True(t, processed)
	require.ErrorIs(t, err, context.Canceled)
	current, err := service.Find(t.Context(), review.Result.NotificationJobUUID)
	require.NoError(t, err)
	require.Equal(t, "running", current.State)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	service = gallery.NewPostMergeNotifications(db.Repository())
	now = now.Add(2 * time.Minute)
	service.Durable.Now = func() time.Time { return now }
	worker = gallery.NewPostMergeNotificationWorker(service, func(ctx context.Context, stored models.PostConsolidationReview, guard gallery.AlbumEffectGuard) error {
		require.NoError(t, guard(ctx))
		seen = append(seen, stored.Request.RequestUUID)
		return nil
	})
	processed, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, []string{review.Request.RequestUUID, review.Request.RequestUUID}, seen)
	current, err = service.Find(t.Context(), review.Result.NotificationJobUUID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", current.State)
	require.Greater(t, current.Fence, int64(1))
}

func TestPostMergeNotificationsExhaustionAndRetryOfRetryKeepOriginalEvent(t *testing.T) {
	db, repo, review := postMergeNotificationFixture(t)
	now := time.Now().UTC()
	service := gallery.NewPostMergeNotifications(repo)
	service.Durable.Now = func() time.Time { return now }
	worker := gallery.NewPostMergeNotificationWorker(service, func(ctx context.Context, stored models.PostConsolidationReview, guard gallery.AlbumEffectGuard) error {
		require.NoError(t, guard(ctx))
		require.Equal(t, *review, stored)
		return errors.New("plugin unavailable")
	})
	for range 8 {
		processed, err := worker.ProcessNext(t.Context())
		require.NoError(t, err)
		require.True(t, processed)
		now = now.Add(time.Minute)
	}
	parent, err := service.Find(t.Context(), review.Result.NotificationJobUUID)
	require.NoError(t, err)
	require.Equal(t, "failed", parent.State)
	child, err := service.Retry(t.Context(), uuid.NewString(), parent.UUID, parent.Revision)
	require.NoError(t, err)
	child, err = service.Cancel(t.Context(), child.UUID, child.Revision)
	require.NoError(t, err)
	request := uuid.NewString()
	last, err := service.Retry(t.Context(), request, child.UUID, child.Revision)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	service = gallery.NewPostMergeNotifications(db.Repository())
	service.Durable.Now = func() time.Time { return now }
	worker = gallery.NewPostMergeNotificationWorker(service, func(ctx context.Context, stored models.PostConsolidationReview, guard gallery.AlbumEffectGuard) error {
		require.Equal(t, *review, stored)
		return guard(ctx)
	})
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	recovered, err := service.Retry(t.Context(), request, child.UUID, child.Revision)
	require.NoError(t, err)
	require.Equal(t, last.UUID, recovered.UUID)
	require.Equal(t, "succeeded", recovered.State)
	history, err := service.History(t.Context(), review.Request.RequestUUID, 0, 2)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, []string{last.UUID, child.UUID}, []string{history[0].UUID, history[1].UUID})
	older, err := service.History(t.Context(), review.Request.RequestUUID, history[1].Sequence, 2)
	require.NoError(t, err)
	require.Len(t, older, 1)
	require.Equal(t, parent.UUID, older[0].UUID)
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, query := range []string{archiveJobWorkHistoryQuery + " ORDER BY id DESC LIMIT 25", archiveJobWorkHistoryQuery + " AND id<99 ORDER BY id DESC LIMIT 25"} {
			_, rows, err := db.QuerySQL(ctx, "EXPLAIN QUERY PLAN "+query, []any{models.ArchiveJobNotifyPostMerge, parent.WorkKey})
			if err != nil {
				return err
			}
			require.Contains(t, fmt.Sprint(rows), "archive_jobs_work_history")
			require.NotContains(t, fmt.Sprint(rows), "SCAN archive_jobs")
		}
		return nil
	}))
}
