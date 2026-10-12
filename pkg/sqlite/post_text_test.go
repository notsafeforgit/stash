package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPostTextPreservesOriginalFillsGapsAndReplays(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "p"}, "")
	var ids []string
	for i, raw := range []string{
		`{"category":"reddit","id":"p","title":"Original title"}`,
		`{"category":"reddit","id":"p","title":"Changed title","selftext":"Original source body"}`,
		`{"category":"reddit","id":"p","title":"Another title","selftext":"Edited source body"}`,
	} {
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", []byte(raw))
		require.NoError(t, err)
		metadata, err := archive.CapturedMetadata([]byte(raw))
		require.NoError(t, err)
		input := models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "gallery-dl", Platform: "reddit",
			CapturedAt: time.Date(2026, 10, 12, 1, i, 0, 0, time.UTC), Metadata: metadata, Payload: *payload}
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			capture, err := repo.SourceEvidence.RecordContentCapture(ctx, input)
			require.NoError(t, err)
			ids = append(ids, capture.UUID)
			return nil
		}))
	}
	require.NotEqual(t, ids[0], ids[1], "a newly available body fills the gap")
	require.Equal(t, ids[1], ids[2], "a caption edit does not create history")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		metadata, err := repo.SourceEvidence.PostContent(ctx, post.UUID)
		require.NoError(t, err)
		require.Equal(t, "Original title", *metadata.Title)
		require.Equal(t, "Original source body", *metadata.OriginalText)
		return nil
	}))
}

func TestPostContentFillsFieldsFromEarlierRecords(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "123"}, "")
	input := contentTestCapture(t, post.UUID, 0, "Saved caption")
	input.Platform = "twitter"
	oldDate, oldBasis := "2026-09-01", "legacy-nfo-unverified"
	input.Metadata.PublishedAt, input.Metadata.DateBasis = &oldDate, &oldBasis
	recordSourceTestCapture(t, repo, input)
	date, basis := "2026-09-01T12:34:56Z", "source"
	input.UUID = uuid.NewString()
	input.Origin = "derived-twitter-id"
	input.CapturedAt = input.CapturedAt.Add(time.Hour)
	input.Metadata = models.SourcePostMetadata{PublishedAt: &date, DateBasis: &basis}
	recordSourceTestCapture(t, repo, input)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		metadata, err := repo.SourceEvidence.PostContent(ctx, post.UUID)
		require.NoError(t, err)
		require.Equal(t, "Saved caption", *metadata.Title)
		require.Equal(t, "Saved caption", *metadata.OriginalText)
		require.Equal(t, date, *metadata.PublishedAt)
		require.Equal(t, basis, *metadata.DateBasis)
		return nil
	}))
}

func TestPostTextPreservesTextAlreadyObtainedByEnrichment(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "p"}, "")
	input := contentTestCapture(t, post.UUID, 0, "Saved source text")
	input.Origin = "gallery-dl-enrichment"
	recordSourceTestCapture(t, repo, input)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		capture, err := repo.SourceEvidence.RecordContentCapture(ctx, contentTestCapture(t, post.UUID, 2, "Edited source text"))
		require.NoError(t, err)
		require.Equal(t, "Saved source text", *capture.Metadata.OriginalText)
		require.NotContains(t, string(capture.Payload.Shared), "Edited source text")
		return nil
	}))
}
