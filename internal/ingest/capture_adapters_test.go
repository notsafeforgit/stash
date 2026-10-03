package ingest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCaptureMirrorIDsKeepServiceAccountAndExtractorProvenance(t *testing.T) {
	f := newCaptureFixtureForNamespace(t, "mirror:coomer:onlyfans")
	var previous *models.IngestReceipt
	for _, account := range []string{"123", "456"} {
		event := f.event(t)
		event.Post = ingest.PostReference{Namespace: "mirror:coomer:onlyfans", Value: account + "/789"}
		body, err := json.Marshal(map[string]any{"category": "coomer", "service": "onlyfans", "user": account, "id": "789", "title": "Source post"})
		require.NoError(t, err)
		event.Source, err = archive.RetainSourcePayload(body)
		require.NoError(t, err)
		event.Metadata, err = archive.CapturedMetadata(event.Source)
		require.NoError(t, err)
		receipt, err := f.submit(t, event)
		require.NoError(t, err)
		if previous != nil {
			require.NotEqual(t, previous.PostUUID, receipt.PostUUID, "the same post ID on another mirror account is a different post")
		}
		require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			capture, err := f.service.Repo.SourceEvidence.FindCapture(ctx, receipt.CaptureUUID)
			require.NoError(t, err)
			require.Equal(t, "coomer", capture.Platform)
			retained, err := archive.RestoreCapture(capture.Payload)
			require.NoError(t, err)
			require.JSONEq(t, string(event.Source), string(retained))
			ref, err := archive.ExtractCapturedPost(retained)
			require.NoError(t, err)
			require.Equal(t, event.Post.Namespace, ref.Namespace)
			require.Equal(t, event.Post.Value, ref.Value)
			return nil
		}))
		require.NoError(t, f.db.Close())
		require.NoError(t, f.db.Open(f.db.DatabasePath()))
		replay, err := f.submit(t, event)
		require.NoError(t, err)
		require.Equal(t, receipt, replay)
		previous = receipt

		event.EventUUID = uuid.NewString()
		event.Post.Value = "other/789"
		_, err = f.submit(t, event)
		require.ErrorIs(t, err, ingest.ErrInvalid, "the declared account must agree with captured evidence")
	}
	// A native service and its mirror never share source authority merely
	// because a publisher/post ID happens to be the same.
	event := f.event(t)
	event.Post = ingest.PostReference{Namespace: "native:fansly", Value: "789"}
	event.Source = []byte(`{"category":"fansly","id":"789","account":{"id":"123"}}`)
	event.Metadata = models.SourcePostMetadata{}
	_, err := f.submit(t, event)
	require.ErrorIs(t, err, ingest.ErrForbidden)
}
