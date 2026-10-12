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
