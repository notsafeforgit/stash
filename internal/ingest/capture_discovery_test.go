package ingest_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCaptureDiscoveryRoutesIgnoreCaptionEditsAndKeepDistinctPosts(t *testing.T) {
	f := newCaptureFixture(t)
	collections := []*models.SourceCollection{f.collection}
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for _, kind := range []string{"account", "subreddit", "search"} {
			collection, err := f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{
				SourceCollectionDefinition: models.SourceCollectionDefinition{Label: kind, Kind: kind, Namespace: "native:reddit", State: "active"}, Origin: "review",
			})
			require.NoError(t, err)
			collections = append(collections, collection)
		}
		return nil
	}))
	scopes := make([]models.IngestScope, len(collections))
	for i, collection := range collections {
		scopes[i].CollectionUUID = collection.UUID
	}
	var err error
	f.credential, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, scopes, nil)
	require.NoError(t, err)
	event := f.event(t)
	var source map[string]any
	require.NoError(t, json.Unmarshal(event.Source, &source))
	var first *models.IngestReceipt
	for i, route := range []string{
		"https://www.reddit.com/user/me/saved/?sort=new",
		"https://www.reddit.com/user/Example/submitted/?sort=new",
		"https://www.reddit.com/r/example/?sort=new",
		"https://www.reddit.com/search?q=author%3AExample&sort=new",
	} {
		event.CollectionUUID = collections[i].UUID
		event.CollectionRevision = collections[i].Revision
		event.EventUUID = uuid.NewString()
		event.ObservedAt = event.ObservedAt.Add(time.Minute)
		source["source_extractor_url"] = route
		source["subcategory"] = collections[i].Kind
		source["saved"] = i == 0
		source["score"] = i
		event.Source, err = json.Marshal(source)
		require.NoError(t, err)
		receipt, err := f.submit(t, event)
		require.NoError(t, err)
		if first == nil {
			first = receipt
		}
		require.Equal(t, first.PostUUID, receipt.PostUUID)
		require.Equal(t, first.CaptureUUID, receipt.CaptureUUID, "a discovery route cannot create another copy of the content")
	}
	history := func() []models.SourceCaptureHistory {
		t.Helper()
		var rows []models.SourceCaptureHistory
		require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			rows, err = f.service.Repo.SourceEvidence.CaptureHistory(ctx, first.PostUUID, "", 25)
			return err
		}))
		return rows
	}
	require.Len(t, history(), 1)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		capture, err := f.service.Repo.SourceEvidence.FindCapture(ctx, first.CaptureUUID)
		require.NoError(t, err)
		require.NotContains(t, string(capture.Payload.Shared), "source_extractor_url")
		require.NotContains(t, string(capture.Payload.Shared), "subcategory")
		return nil
	}))

	// Changed text alone does not create another capture; new album evidence
	// remains usable independently of the caption.
	title := "Edited album caption"
	event.Metadata.Title = &title
	source["title"] = title
	for _, changeAlbum := range []bool{false, true} {
		if changeAlbum {
			source["gallery_data"].(map[string]any)["items"] = []any{map[string]any{"media_id": "image1"}}
		}
		event.EventUUID = uuid.NewString()
		event.ObservedAt = event.ObservedAt.Add(time.Minute)
		event.Source, err = json.Marshal(source)
		require.NoError(t, err)
		receipt, err := f.submit(t, event)
		require.NoError(t, err)
		require.Equal(t, first.PostUUID, receipt.PostUUID)
		if changeAlbum {
			require.NotEqual(t, first.CaptureUUID, receipt.CaptureUUID)
		} else {
			require.Equal(t, first.CaptureUUID, receipt.CaptureUUID)
		}
		var result ingest.CaptureResult
		require.NoError(t, json.Unmarshal(receipt.Result, &result))
		require.Equal(t, "committed", result.Status)
	}
	require.Len(t, history(), 2, "only the changed attachment list needs new evidence")
	// Reusing the same media under another source post ID remains a distinct
	// post with its own caption and attachment membership.
	event.Post.Value = "post2"
	source["id"] = "post2"
	event.EventUUID = uuid.NewString()
	event.Source, err = json.Marshal(source)
	require.NoError(t, err)
	repost, err := f.submit(t, event)
	require.NoError(t, err)
	require.NotEqual(t, first.PostUUID, repost.PostUUID)
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		original, err := f.service.Repo.SourceEvidence.PostContent(ctx, first.PostUUID)
		require.NoError(t, err)
		require.Equal(t, "Source album", *original.Title)
		other, err := f.service.Repo.SourceEvidence.PostContent(ctx, repost.PostUUID)
		require.NoError(t, err)
		require.Equal(t, title, *other.Title)
		return nil
	}))
}
