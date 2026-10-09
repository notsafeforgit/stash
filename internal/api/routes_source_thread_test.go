package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceThreadHTTPNavigation(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	var post *models.SourcePost
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "103"}, "")
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "twitter", []byte(`{"category":"twitter","tweet_id":"103","conversation_id":"100","reply_id":"102","reply_user_id":"99","author":{"id":"99"}}`))
		if err != nil {
			return err
		}
		capture, err := repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "gallery-dl", Platform: "twitter", CapturedAt: time.Now(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
		if err != nil {
			return err
		}
		_, err = repo.SourceThread.ObserveCapture(ctx, capture)
		return err
	}))
	var result struct {
		Requested string                  `json:"requested_uuid"`
		Thread    models.SourceThreadView `json:"thread"`
	}
	require.NoError(t, json.Unmarshal(get("/posts/"+post.UUID+"/thread?limit=1", 200).Body.Bytes(), &result))
	require.Equal(t, post.UUID, result.Requested)
	require.Equal(t, "102", result.Thread.Parent.SourceID)
	require.Nil(t, result.Thread.Parent.Post)
	require.Equal(t, post.UUID, result.Thread.Posts[0].Post.UUID)
	for _, query := range []string{"after=bad", "after=1e3", "limit=0", "limit=101"} {
		get("/posts/"+post.UUID+"/thread?"+query, 400)
	}
	get("/posts/"+uuid.NewString()+"/thread", 404)
}
