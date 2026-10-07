package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSourcePostReviewHTTPSharedCapturesAndTargetedPagination(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "source-review.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var post *models.SourcePost
	var media *models.ArchiveEntity
	clock := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if _, _, err := db.ExecSQL(ctx, `INSERT INTO images(id,created_at,updated_at) VALUES(1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, nil); err != nil {
			return err
		}
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "review-http"}, "")
		if err != nil {
			return err
		}
		media, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveImage, 1)
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", []byte(`{"category":"reddit","id":"review-http","title":"Shared text"}`))
		if err != nil {
			return err
		}
		title := "Shared text"
		for i := range 3 {
			input := models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "gallery-dl", Platform: "reddit",
				CapturedAt: clock.Add(time.Duration(i) * time.Hour), RetentionPolicy: archive.SourceRetentionVersion,
				Metadata: models.SourcePostMetadata{Title: &title}, Payload: *payload}
			if i == 2 {
				recorded := input.CapturedAt
				input.CapturedAt, input.RecordedAt = time.Time{}, &recorded
			}
			if _, err := repo.SourceEvidence.RecordCapture(ctx, input); err != nil {
				return err
			}
		}
		_, err = repo.SourceAttachment.RecordMediaEvidence(ctx, models.SourceMediaEvidence{
			UUID: uuid.NewString(), PostUUID: post.UUID, MediaUUID: media.UUID, Basis: "legacy", Details: []byte(`{}`)})
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	get := func(path string, status int) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, status, w.Code, w.Body.String())
		return w
	}
	base := "/entities/" + media.UUID + "/source-posts"
	w := get(base+"?limit=1", 200)
	var posts []models.SourcePostMediaReview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &posts))
	require.Len(t, posts, 1)
	require.Equal(t, "undecided", posts[0].Association.State)
	require.True(t, posts[0].HasRetainedEvidence)
	require.Nil(t, posts[0].LatestCapture.CapturedAt)
	require.NotContains(t, w.Body.String(), "payload")
	get("/posts/"+post.UUID+"/media/"+media.UUID+"/review", 200)
	require.JSONEq(t, `[]`, get(base+"?limit=1&after="+post.UUID, 200).Body.String())
	for _, query := range []string{"limit=0", "limit=101", "limit=bad", "after=bad"} {
		get(base+"?"+query, 400)
	}
	path := "/posts/" + post.UUID + "/capture-summaries"
	w = get(path+"?limit=3", 200)
	var page struct {
		Captures  []sourceReviewCapture  `json:"captures"`
		Revisions []sourceReviewRevision `json:"revisions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Captures, 3)
	require.Len(t, page.Revisions, 1, "shared text appears once, irrespective of observation count")
	require.Equal(t, "Shared text", *page.Revisions[0].Metadata.Title)
	require.NotContains(t, w.Body.String(), "payload")
	require.NotContains(t, w.Body.String(), "settings")
	last := page.Captures[2]
	require.Nil(t, last.CapturedAt)
	require.NotNil(t, last.RecordedAt)
	query := url.Values{"after_uuid": {last.UUID}, "after_clock": {"recorded"}, "after_time": {last.RecordedAt.Format(time.RFC3339Nano)}}
	w = get(path+"?"+query.Encode(), 200)
	require.JSONEq(t, `{"requested_uuid":"`+post.UUID+`","captures":[],"revisions":[]}`, w.Body.String())
	query.Set("after_clock", "unverified")
	get(path+"?"+query.Encode(), 400)
	get(path+"?after_uuid="+last.UUID, 400)
	get("/posts/"+post.UUID+"/urls?after=invalid", 400)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		choice, err := repo.SourcePostMedia.Association(ctx, post.UUID, media.UUID)
		require.NoError(t, err)
		require.Empty(t, choice.Decisions, "inspection never accepts a candidate")
		return nil
	}))
}
