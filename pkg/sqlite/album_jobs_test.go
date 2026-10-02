package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func albumJobInput(t *testing.T, service *gallery.AlbumBackfill, post string) gallery.AlbumBackfillRequest {
	t.Helper()
	preview, err := service.Preview(t.Context(), post, models.SourceAlbumRedditFilenameV1)
	require.NoError(t, err)
	return gallery.AlbumBackfillRequest{RequestUUID: uuid.NewString(), PostUUID: post, Policy: preview.Policy, Signature: preview.Signature}
}

func albumJobStatus(t *testing.T, service *gallery.AlbumBackfill, id string) *gallery.AlbumBackfillStatus {
	t.Helper()
	status, err := service.Status(t.Context(), id, false)
	require.NoError(t, err)
	return status
}

func processAlbumJob(t *testing.T, worker *gallery.AlbumWorker) {
	t.Helper()
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
}

func TestAlbumJobsAdmissionReplayAndTargetedHistory(t *testing.T) {
	f, post, _ := singleBackfillFixture(t)
	s := gallery.NewAlbumBackfill(f.repo)
	s.Durable.MaxActive = 1
	input := albumJobInput(t, s, post)
	first, err := s.Submit(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, "queued", first.State)
	require.False(t, first.PublicationCommitted)
	require.False(t, first.HooksFinished)
	require.Equal(t, "create", sourceGalleryPreview(t, f.repo, post).Action)
	coalesced := input
	coalesced.RequestUUID = uuid.NewString()
	second, err := s.Submit(t.Context(), coalesced)
	require.NoError(t, err)
	require.Equal(t, first, second)
	changed := input
	changed.Policy = models.SourceAlbumIdentifiersV1
	_, err = s.Submit(t.Context(), changed)
	require.ErrorIs(t, err, models.ErrArchiveJobConflict)
	other := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "other"}, "")
	_, err = s.Submit(t.Context(), albumJobInput(t, s, other.UUID))
	require.ErrorIs(t, err, models.ErrArchiveJobCapacity)
	calls := 0
	worker := gallery.NewAlbumWorker(s, func(ctx context.Context, pub gallery.AlbumPublication, guard gallery.AlbumEffectGuard) error {
		calls++
		current := albumJobStatus(t, s, first.JobUUID)
		require.True(t, current.PublicationCommitted, "effects see committed publication outside its write transaction")
		require.False(t, current.HooksFinished)
		require.Equal(t, &pub, current.Publication)
		require.Equal(t, first.JobUUID, pub.EventUUID)
		require.True(t, pub.Created)
		require.Equal(t, 1, pub.Selected)
		require.Equal(t, 1, pub.Added)
		return guard(ctx)
	})
	processAlbumJob(t, worker)
	final := albumJobStatus(t, s, first.JobUUID)
	require.Equal(t, "succeeded", final.State)
	require.True(t, final.HooksFinished)
	require.Equal(t, 1, calls)
	for _, request := range []gallery.AlbumBackfillRequest{input, coalesced} {
		replay, err := s.Submit(t.Context(), request)
		require.NoError(t, err)
		require.Equal(t, final, replay, "lost responses replay after the original preview became stale")
		inspected, err := s.Status(t.Context(), request.RequestUUID, true)
		require.NoError(t, err)
		require.Equal(t, final, inspected)
	}
	next, err := s.Submit(t.Context(), albumJobInput(t, s, post))
	require.NoError(t, err)
	history, err := s.History(t.Context(), post, 0, 1)
	require.NoError(t, err)
	require.Equal(t, []gallery.AlbumBackfillStatus{*final}, history)
	history, err = s.History(t.Context(), post, final.Sequence, 1)
	require.NoError(t, err)
	require.Equal(t, []gallery.AlbumBackfillStatus{*next}, history)
	history, err = s.History(t.Context(), other.UUID, 0, 100)
	require.NoError(t, err)
	require.Empty(t, history)
	_, err = s.History(t.Context(), post, 0, 101)
	require.ErrorIs(t, err, gallery.ErrAlbumWorkInvalid)
	attempts, err := s.Attempts(t.Context(), first.JobUUID, 0, 1)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, "succeeded", attempts[0].Outcome)
	attempts, err = s.Attempts(t.Context(), first.JobUUID, attempts[0].Fence, 1)
	require.NoError(t, err)
	require.Empty(t, attempts)
	_, err = s.Status(t.Context(), uuid.NewString(), false)
	require.ErrorIs(t, err, gallery.ErrAlbumWorkNotFound)
}

func TestAlbumJobsStalePreviewAndUnpublishedRetry(t *testing.T) {
	for _, when := range []string{"before admission", "after admission", "cancelled before publication"} {
		t.Run(when, func(t *testing.T) {
			f, post, _ := singleBackfillFixture(t)
			s := gallery.NewAlbumBackfill(f.repo)
			input := albumJobInput(t, s, post)
			var accepted *gallery.AlbumBackfillStatus
			var err error
			if when != "before admission" {
				accepted, err = s.Submit(t.Context(), input)
				require.NoError(t, err)
				if when == "cancelled before publication" {
					accepted, err = s.Cancel(t.Context(), accepted.JobUUID, accepted.Revision)
					require.NoError(t, err)
				}
			}
			attachmentSQL(t, f.db, "UPDATE files SET size=1001 WHERE id=21")
			switch when {
			case "before admission":
				_, err := s.Submit(t.Context(), input)
				require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
			case "cancelled before publication":
				_, err := s.Retry(t.Context(), accepted.JobUUID, accepted.Revision, uuid.NewString())
				require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
			default:
				worker := gallery.NewAlbumWorker(s, func(context.Context, gallery.AlbumPublication, gallery.AlbumEffectGuard) error {
					t.Error("stale preview reached effects")
					return nil
				})
				processAlbumJob(t, worker)
				status := albumJobStatus(t, s, accepted.JobUUID)
				require.Equal(t, "failed", status.State)
				require.Equal(t, "album_preview_changed", status.ErrorCode)
				require.False(t, status.PublicationCommitted)
			}
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM galleries"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM attachment_media_decisions"))
		})
	}
}

func TestAlbumJobsCheckpointFailureRollsBackDomain(t *testing.T) {
	f, post, _ := singleBackfillFixture(t)
	s := gallery.NewAlbumBackfill(f.repo)
	now := time.Now().UTC()
	s.Durable.Now = func() time.Time { return now }
	input := albumJobInput(t, s, post)
	accepted, err := s.Submit(t.Context(), input)
	require.NoError(t, err)
	attachmentSQL(t, f.db, `CREATE TRIGGER reject_album_progress BEFORE UPDATE OF progress ON archive_jobs
 WHEN json_extract(NEW.progress,'$.publication') IS NOT NULL BEGIN SELECT RAISE(ABORT,'injected checkpoint failure'); END;`)
	calls := 0
	worker := gallery.NewAlbumWorker(s, func(context.Context, gallery.AlbumPublication, gallery.AlbumEffectGuard) error { calls++; return nil })
	processAlbumJob(t, worker)
	require.Zero(t, calls)
	status := albumJobStatus(t, s, accepted.JobUUID)
	require.Equal(t, "queued", status.State)
	require.False(t, status.PublicationCommitted)
	require.Equal(t, input.Signature, albumJobInput(t, s, post).Signature)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM galleries"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM attachment_media_decisions"))
	attachmentSQL(t, f.db, "DROP TRIGGER reject_album_progress")
	now = status.AvailableAt
	processAlbumJob(t, worker)
	require.Equal(t, 1, calls)
	require.True(t, albumJobStatus(t, s, accepted.JobUUID).HooksFinished)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM galleries"))
}

func TestAlbumJobsTerminalRetryKeepsPublicationAndManualChanges(t *testing.T) {
	f, post, _ := singleBackfillFixture(t)
	s := gallery.NewAlbumBackfill(f.repo)
	now := time.Now().UTC()
	s.Durable.Now = func() time.Time { return now }
	accepted, err := s.Submit(t.Context(), albumJobInput(t, s, post))
	require.NoError(t, err)
	var original gallery.AlbumPublication
	worker := gallery.NewAlbumWorker(s, func(_ context.Context, pub gallery.AlbumPublication, _ gallery.AlbumEffectGuard) error {
		if original.EventUUID == "" {
			original = pub
		}
		require.Equal(t, original, pub)
		return errors.New("temporary notification failure")
	})
	var terminal *gallery.AlbumBackfillStatus
	for range accepted.MaxAttempts {
		processAlbumJob(t, worker)
		terminal = albumJobStatus(t, s, accepted.JobUUID)
		now = terminal.AvailableAt.Add(time.Second)
	}
	require.Equal(t, "failed", terminal.State)
	require.True(t, terminal.PublicationCommitted)
	require.False(t, terminal.HooksFinished)
	attachmentSQL(t, f.db, "UPDATE galleries SET title='User title'; DELETE FROM scenes_galleries")
	_, err = s.Retry(t.Context(), terminal.JobUUID, terminal.Revision-1, uuid.NewString())
	require.ErrorIs(t, err, models.ErrArchiveJobConflict)
	request := uuid.NewString()
	resumed, err := s.Retry(t.Context(), terminal.JobUUID, terminal.Revision, request)
	require.NoError(t, err)
	require.True(t, resumed.PublicationCommitted, "a queued notification retry retains the prior committed publication")
	require.Equal(t, original, *resumed.Publication)
	replay, err := s.Retry(t.Context(), terminal.JobUUID, terminal.Revision, request)
	require.NoError(t, err)
	require.Equal(t, resumed, replay)
	// Cancel a retry before its first checkpoint, then resume it. This must not
	// lose the publication merely because the intermediate job never ran.
	cancelled, err := s.Cancel(t.Context(), resumed.JobUUID, resumed.Revision)
	require.NoError(t, err)
	require.True(t, cancelled.PublicationCommitted)
	last, err := s.Retry(t.Context(), cancelled.JobUUID, cancelled.Revision, uuid.NewString())
	require.NoError(t, err)
	worker.Effects = func(ctx context.Context, pub gallery.AlbumPublication, guard gallery.AlbumEffectGuard) error {
		require.Equal(t, original, pub)
		return guard(ctx)
	}
	processAlbumJob(t, worker)
	require.True(t, albumJobStatus(t, s, last.JobUUID).HooksFinished)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM galleries WHERE title='User title'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM scenes_galleries"), "notification retry cannot re-add excluded media")
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM attachment_media_decisions"))
	require.Equal(t, terminal, albumJobStatus(t, s, terminal.JobUUID))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestAlbumJobsRestartResumesCommittedEffects(t *testing.T) {
	f, post, _ := singleBackfillFixture(t)
	s := gallery.NewAlbumBackfill(f.repo)
	now := time.Now().UTC()
	s.Durable.Now = func() time.Time { return now }
	accepted, err := s.Submit(t.Context(), albumJobInput(t, s, post))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	var original gallery.AlbumPublication
	worker := gallery.NewAlbumWorker(s, func(_ context.Context, pub gallery.AlbumPublication, _ gallery.AlbumEffectGuard) error {
		original = pub
		cancel()
		return ctx.Err()
	})
	processed, err := worker.ProcessNext(ctx)
	require.True(t, processed)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "running", albumJobStatus(t, s, accepted.JobUUID).State)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	s = gallery.NewAlbumBackfill(f.db.Repository())
	now = now.Add(2 * time.Minute)
	s.Durable.Now = func() time.Time { return now }
	worker = gallery.NewAlbumWorker(s, func(ctx context.Context, pub gallery.AlbumPublication, guard gallery.AlbumEffectGuard) error {
		require.Equal(t, original, pub)
		return guard(ctx)
	})
	processAlbumJob(t, worker)
	status := albumJobStatus(t, s, accepted.JobUUID)
	require.True(t, status.HooksFinished)
	require.EqualValues(t, 2, status.Attempts)
	attempts, err := s.Attempts(t.Context(), status.JobUUID, 0, 100)
	require.NoError(t, err)
	require.Len(t, attempts, 2)
	require.Equal(t, "expired", attempts[0].Outcome)
	require.Equal(t, "succeeded", attempts[1].Outcome)
}

func TestAlbumJobsCancellationAfterCommitFencesEffects(t *testing.T) {
	f, post, _ := singleBackfillFixture(t)
	s := gallery.NewAlbumBackfill(f.repo)
	accepted, err := s.Submit(t.Context(), albumJobInput(t, s, post))
	require.NoError(t, err)
	var cancelled *gallery.AlbumBackfillStatus
	var oldGuard gallery.AlbumEffectGuard
	worker := gallery.NewAlbumWorker(s, func(ctx context.Context, _ gallery.AlbumPublication, guard gallery.AlbumEffectGuard) error {
		oldGuard = guard
		current := albumJobStatus(t, s, accepted.JobUUID)
		var err error
		cancelled, err = s.Cancel(ctx, accepted.JobUUID, current.Revision)
		require.NoError(t, err)
		return guard(ctx)
	})
	processed, err := worker.ProcessNext(t.Context())
	require.True(t, processed)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	require.Equal(t, "cancelled", cancelled.State)
	require.True(t, cancelled.PublicationCommitted)
	resumed, err := s.Retry(t.Context(), cancelled.JobUUID, cancelled.Revision, uuid.NewString())
	require.NoError(t, err)
	worker.Effects = func(ctx context.Context, _ gallery.AlbumPublication, guard gallery.AlbumEffectGuard) error {
		return guard(ctx)
	}
	processAlbumJob(t, worker)
	require.True(t, albumJobStatus(t, s, resumed.JobUUID).HooksFinished)
	require.ErrorIs(t, oldGuard(t.Context()), models.ErrArchiveJobLease)
}

func TestAlbumJobsHeartbeatRenewsAndCancelsOnLostOwnership(t *testing.T) {
	f, post, _ := singleBackfillFixture(t)
	s := gallery.NewAlbumBackfill(f.repo)
	accepted, err := s.Submit(t.Context(), albumJobInput(t, s, post))
	require.NoError(t, err)
	worker := gallery.NewAlbumWorker(s, func(ctx context.Context, _ gallery.AlbumPublication, _ gallery.AlbumEffectGuard) error {
		initial := albumJobStatus(t, s, accepted.JobUUID)
		require.Eventually(t, func() bool {
			return albumJobStatus(t, s, accepted.JobUUID).Revision > initial.Revision
		}, 8*time.Second, 20*time.Millisecond)
		current := albumJobStatus(t, s, accepted.JobUUID)
		_, err := s.Cancel(ctx, accepted.JobUUID, current.Revision)
		require.NoError(t, err)
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-time.After(8 * time.Second):
			return errors.New("lost lease did not cancel effects")
		}
	})
	worker.LeaseDuration = 5 * time.Second
	processed, err := worker.ProcessNext(t.Context())
	require.True(t, processed)
	require.ErrorIs(t, err, models.ErrArchiveJobLease)
	require.False(t, albumJobStatus(t, s, accepted.JobUUID).HooksFinished)
}

func TestAlbumJobsJSONPreviewUsesPublicNames(t *testing.T) {
	f, post, _ := singleBackfillFixture(t)
	s := gallery.NewAlbumBackfill(f.repo)
	preview, err := s.Preview(t.Context(), post, models.SourceAlbumRedditFilenameV1)
	require.NoError(t, err)
	raw, err := json.Marshal(preview)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal(raw, &data))
	match := data["matches"].([]any)[0].(map[string]any)
	require.Equal(t, map[string]any{"namespace": "native:reddit", "value": "cd456"}, match["reference"])
	require.NotContains(t, string(raw), "OriginalID")
	require.NotContains(t, string(raw), "Signature")
}
