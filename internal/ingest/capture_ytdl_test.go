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

func TestCaptureYTDLGenericPagesKeepSeparateVideosAndSharedPublisher(t *testing.T) {
	f := newCaptureFixtureForNamespace(t, "ytdl:thisvid.com")
	var posts, publishers []string
	for _, page := range []string{"first", "second", "first"} {
		data := map[string]interface{}{"category": "ytdl", "subcategory": "ThisVidMember", "extractor_key": "Generic",
			"id": "3533241", "webpage_url": "https://thisvid.com/videos/" + page + "/", "uploader_id": "150629",
			"uploader": "Publisher", "title": "Undated video", "playlist_id": "different-collector",
			"ytdl_media": map[string]interface{}{"version": 1, "type": "video"}}
		raw, err := json.Marshal(data)
		require.NoError(t, err)
		raw, err = archive.RetainSourcePayload(raw)
		require.NoError(t, err)
		post, err := archive.ExtractCapturedPost(raw)
		require.NoError(t, err)
		event := f.event(t)
		event.EventUUID, event.Source = uuid.NewString(), raw
		event.Post = ingest.PostReference{Namespace: post.Namespace, Value: post.Value}
		event.Metadata, err = archive.CapturedMetadata(raw)
		require.NoError(t, err)
		require.Nil(t, event.Metadata.PublishedAt)
		receipt, err := f.submit(t, event)
		require.NoError(t, err)
		replay, err := f.submit(t, event)
		require.NoError(t, err)
		require.Equal(t, receipt, replay)
		posts = append(posts, receipt.PostUUID)
		require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			selection, err := f.service.Repo.SourceAttachment.Selection(ctx, receipt.PostUUID)
			require.NoError(t, err)
			require.Len(t, selection.Entries, 1)
			require.True(t, selection.Complete)
			require.False(t, selection.DeclaredAlbum)
			require.Equal(t, models.SourcePostIdentifier{Namespace: post.Namespace, Value: post.Value}, selection.Entries[0].Attachment.Reference)
			publisher, err := f.service.Repo.CapturePublisher.Current(ctx, receipt.CaptureUUID)
			require.NoError(t, err)
			require.NotNil(t, publisher.AccountUUID)
			publishers = append(publishers, *publisher.AccountUUID)
			owner, err := f.service.Repo.SourceAccount.Ownership(ctx, *publisher.AccountUUID)
			require.NoError(t, err)
			require.Nil(t, owner, "collector and publisher evidence cannot assign a depicted performer")
			return nil
		}))
	}
	require.NotEqual(t, posts[0], posts[1], "the same Generic ID on another page is a different video")
	require.Equal(t, posts[0], posts[2], "repeated page captures reuse the original post")
	require.Equal(t, publishers[0], publishers[1])
	require.Equal(t, publishers[0], publishers[2])
}
