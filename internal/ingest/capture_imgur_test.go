package ingest_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCaptureImgurAlbumMembershipAndReceiptReplay(t *testing.T) {
	f := newCaptureFixture(t)
	raw, err := os.ReadFile("../../pkg/archive/testdata/reddit-imgur-albums-v1.json")
	require.NoError(t, err)
	var fixture struct {
		Cases []struct{ Source json.RawMessage }
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	event := f.event(t)
	event.Source, event.Metadata = fixture.Cases[0].Source, models.SourcePostMetadata{}
	event.Post = ingest.PostReference{Namespace: "native:reddit", Value: "post"}
	first, err := f.submit(t, event)
	require.NoError(t, err)
	replay, err := f.submit(t, event)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		selection, err := f.service.Repo.SourceAttachment.Selection(ctx, first.PostUUID)
		require.NoError(t, err)
		require.True(t, selection.Complete)
		require.True(t, selection.DeclaredAlbum)
		require.Equal(t, 2, *selection.ExpectedCount)
		require.Len(t, selection.Entries, 2)
		for i, id := range []string{"First12", "Next123"} {
			require.Equal(t, i, selection.Entries[i].Position)
			require.Equal(t, models.SourcePostIdentifier{Namespace: "native:imgur", Value: id}, selection.Entries[i].Attachment.Reference)
		}
		return nil
	}))
}
