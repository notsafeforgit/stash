package ingest_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCaptureStoresOnePostURLAcrossMediaCapturesAndReceiptReplay(t *testing.T) {
	f := newCaptureFixture(t)
	event := f.event(t)
	event.Source = []byte(`{"category":"reddit","id":"post1","permalink":"/r/test/comments/post1/title/","url":"https://i.redd.it/media.jpg","num":1}`)
	first, err := f.submit(t, event)
	require.NoError(t, err)
	replay, err := f.submit(t, event)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	event.EventUUID = uuid.NewString()
	event.ObservedAt = event.ObservedAt.Add(time.Second)
	event.Source = []byte(`{"category":"reddit","id":"post1","permalink":"/r/test/comments/post1/title/","url":"https://i.redd.it/another.jpg","num":2}`)
	_, err = f.submit(t, event)
	require.NoError(t, err)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		urls, err := f.service.Repo.SourcePostLinks.CurrentURLs(ctx, first.PostUUID, "", 100)
		require.NoError(t, err)
		require.Len(t, urls, 1)
		require.Equal(t, "https://www.reddit.com/r/test/comments/post1/title/", urls[0].URL)
		evidence, err := f.service.Repo.SourcePostLinks.URLEvidence(ctx, urls[0].UUID, "", 100)
		require.NoError(t, err)
		require.Len(t, evidence, 1)
		require.JSONEq(t, `{"capture_uuid":"`+first.CaptureUUID+`"}`, string(evidence[0].Details))
		summary, err := f.service.Repo.SourceEvidence.PostSummary(ctx, first.PostUUID)
		require.NoError(t, err)
		require.Equal(t, urls, summary.URLs, "the source-post browser uses the persisted link")
		return nil
	}))
}

func TestCaptureURLBackfillPreservesContentAndIsAtomic(t *testing.T) {
	f := newCaptureFixtureForNamespace(t, "native:twitter")
	var capture *models.SourceCapture
	var post *models.SourcePost
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = f.service.Repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "123456789"}, "")
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "twitter", []byte(`{"category":"twitter","tweet_id":"123456789","content":"Original post text"}`))
		if err != nil {
			return err
		}
		capture, err = f.service.Repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID,
			Origin: "gallery-dl", Platform: "twitter", CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
		return err
	}))
	_, err := f.service.BackfillCaptureURLs(t.Context(), []string{capture.UUID, uuid.NewString()})
	require.ErrorIs(t, err, ingest.ErrNotFound)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		urls, err := f.service.Repo.SourcePostLinks.CurrentURLs(ctx, post.UUID, "", 100)
		require.NoError(t, err)
		require.Empty(t, urls, "failure rolls back the entire repair batch")
		return nil
	}))
	for _, status := range []string{"recovered", "preserved"} {
		result, err := f.service.BackfillCaptureURLs(t.Context(), []string{capture.UUID})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Equal(t, status, result[0].Status)
		require.Equal(t, "https://x.com/i/web/status/123456789", result[0].URL.URL)
	}
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		unchanged, err := f.service.Repo.SourceEvidence.FindCapture(ctx, capture.UUID)
		require.NoError(t, err)
		require.Equal(t, capture, unchanged)
		rows, err := f.service.Repo.SourceEvidence.CurrentCaptures(ctx, post.UUID, nil, 100)
		require.NoError(t, err)
		require.Len(t, rows, 1, "repair does not invent another capture")
		return nil
	}))
}
