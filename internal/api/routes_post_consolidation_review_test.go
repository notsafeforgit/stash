package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type postMergeHTTPStatus struct {
	Sequence              int64  `json:"sequence"`
	JobUUID               string `json:"job_uuid"`
	ReviewUUID            string `json:"review_uuid"`
	State                 string `json:"state"`
	Revision              int64  `json:"revision"`
	HooksFinished         bool   `json:"hooks_finished"`
	ResumeFromJobUUID     string `json:"resume_from_job_uuid"`
	ResumeFromJobRevision int64  `json:"resume_from_job_revision"`
}

func TestPostConsolidationHTTPApplyRecoveryNotificationRetryAndShutdown(t *testing.T) {
	db, repo, _ := sourcePostBrowserHTTPFixture(t)
	post := albumHTTPPost(t, repo)
	input := models.PostConsolidationReviewInput{DestinationUUID: post.UUID, Reason: "Same original album"}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		legacy, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "legacy"}, "")
		if err == nil {
			input.SourceUUID = legacy.UUID
		}
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(method, path string, body any, expected int) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, expected, w.Code, w.Body.String())
		return w
	}
	var preview models.PostConsolidationReviewPreview
	w := request(http.MethodPost, "/post-consolidation/preview", input, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	require.True(t, preview.Ready)
	require.Equal(t, "create", preview.Album.Action)
	for _, absent := range []string{"payload", "settings", "request_uuid"} {
		require.NotContains(t, w.Body.String(), `"`+absent+`"`)
	}
	apply := models.PostConsolidationReviewApplyInput{PostConsolidationReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
	path := "/post-consolidation/requests/" + apply.RequestUUID
	request(http.MethodPost, path+"/check", apply, 404)
	request(http.MethodGet, path, nil, 404)
	w = request(http.MethodGet, "/posts/"+input.SourceUUID+"/consolidation-history", nil, 200)
	require.JSONEq(t, "[]", w.Body.String())
	var first models.PostConsolidationReview
	for i := range 2 {
		w = request(http.MethodPost, "/post-consolidation/apply", apply, 200)
		var result struct {
			Review   models.PostConsolidationReview `json:"review"`
			Replayed bool                           `json:"replayed"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, i == 1, result.Replayed)
		if i == 0 {
			first = result.Review
		} else {
			require.Equal(t, first, result.Review)
		}
	}
	require.True(t, first.Result.Gallery.Created)
	var recovered models.PostConsolidationReview
	w = request(http.MethodPost, path+"/check", apply, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recovered))
	require.Equal(t, first, recovered)
	var history []models.SourcePostConsolidation
	w = request(http.MethodGet, "/posts/"+input.SourceUUID+"/consolidation-history?limit=1", nil, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.Equal(t, []models.SourcePostConsolidation{first.Result.Consolidation}, history)
	changed := apply
	changed.Reason = "Another saved request"
	for _, route := range []string{path + "/check", "/post-consolidation/apply"} {
		require.Contains(t, request(http.MethodPost, route, changed, 409).Body.String(), "request_conflict")
	}
	changed = apply
	changed.RequestUUID = uuid.NewString()
	request(http.MethodPost, path+"/check", changed, 400)
	require.Contains(t, request(http.MethodPost, "/post-consolidation/apply", changed, 409).Body.String(), "preview_changed")

	jobPath := "/post-merge-notifications/" + first.Result.NotificationJobUUID
	var original, cancelled, retry postMergeHTTPStatus
	w = request(http.MethodGet, jobPath, nil, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &original))
	require.Equal(t, "queued", original.State)
	require.False(t, original.HooksFinished)
	require.NotContains(t, w.Body.String(), "arguments")
	require.NotContains(t, w.Body.String(), "work_key")
	request(http.MethodPost, jobPath+"/cancel", map[string]int64{"expected_revision": original.Revision + 1}, 409)
	w = request(http.MethodPost, jobPath+"/cancel", map[string]int64{"expected_revision": original.Revision}, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cancelled))
	require.Equal(t, "cancelled", cancelled.State)
	retryRequest := map[string]any{"request_uuid": uuid.NewString(), "expected_revision": cancelled.Revision}
	w = request(http.MethodPost, jobPath+"/retry", retryRequest, 202)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &retry))
	require.Equal(t, cancelled.JobUUID, retry.ResumeFromJobUUID)
	require.Equal(t, cancelled.Revision, retry.ResumeFromJobRevision)
	require.Equal(t, apply.RequestUUID, retry.ReviewUUID)
	service := gallery.NewPostMergeNotifications(repo)
	events := make(chan string, 2)
	worker := gallery.NewPostMergeNotificationWorker(service, func(ctx context.Context, review models.PostConsolidationReview, guard gallery.AlbumEffectGuard) error {
		if err := guard(ctx); err != nil {
			return err
		}
		events <- review.Request.RequestUUID
		return nil
	})
	runtime := &archiveWorkerRuntime{worker: worker}
	t.Cleanup(runtime.stop)
	runtime.start()
	runtime.start()
	select {
	case event := <-events:
		require.Equal(t, apply.RequestUUID, event)
	case <-time.After(10 * time.Second):
		t.Fatal("merge notification worker did not deliver")
	}
	require.Eventually(t, func() bool {
		current, err := service.Find(t.Context(), retry.JobUUID)
		return err == nil && current.State == "succeeded"
	}, 10*time.Second, 20*time.Millisecond)
	server := &Server{postMergeNotifications: runtime}
	server.Shutdown()
	runtime.start()
	select {
	case <-runtime.done:
	default:
		t.Fatal("merge notification worker outlived server shutdown")
	}
	require.Empty(t, events, "starting twice cannot create another delivery loop")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	handler = (&nativeArchiveRoutes{repo: repo}).router()
	w = request(http.MethodPost, path+"/check", apply, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recovered))
	require.Equal(t, first, recovered)
	w = request(http.MethodPost, jobPath+"/retry", retryRequest, 200)
	var done postMergeHTTPStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &done))
	require.Equal(t, retry.JobUUID, done.JobUUID)
	require.True(t, done.HooksFinished)
	w = request(http.MethodGet, "/post-merge-notification-requests/"+retryRequest["request_uuid"].(string), nil, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &done))
	require.Equal(t, retry.JobUUID, done.JobUUID)
	request(http.MethodGet, "/post-merge-notifications/"+uuid.NewString(), nil, 404)
	request(http.MethodGet, "/post-merge-notifications/invalid", nil, 400)
	var notifications []postMergeHTTPStatus
	notificationsPath := path + "/notifications"
	w = request(http.MethodGet, notificationsPath+"?limit=1", nil, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &notifications))
	require.Len(t, notifications, 1)
	require.Equal(t, done, notifications[0])
	require.Greater(t, done.Sequence, original.Sequence)
	w = request(http.MethodGet, notificationsPath+"?limit=1&before="+strconv.FormatInt(done.Sequence, 10), nil, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &notifications))
	require.Equal(t, []postMergeHTTPStatus{cancelled}, notifications)
	request(http.MethodGet, notificationsPath+"?before=-1", nil, 400)
	request(http.MethodGet, notificationsPath+"?limit=101", nil, 400)
	request(http.MethodGet, "/post-consolidation/requests/"+uuid.NewString()+"/notifications", nil, 404)
}

func TestPostConsolidationHTTPRejectsStaleMalformedAndCrossOriginRequests(t *testing.T) {
	_, repo, _ := sourcePostBrowserHTTPFixture(t)
	input := models.PostConsolidationReviewInput{}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for i, target := range []*string{&input.SourceUUID, &input.DestinationUUID} {
			post, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: string(rune('a' + i))}, "")
			if err != nil {
				return err
			}
			*target = post.UUID
		}
		return nil
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	var preview *models.PostConsolidationReviewPreview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		preview, err = repo.SourceEvidence.PreviewConsolidationReview(ctx, input)
		return err
	}))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		post, err := repo.SourceEvidence.FindPost(ctx, input.DestinationUUID)
		if err != nil {
			return err
		}
		return repo.SourceEvidence.AddPostIdentifier(ctx, post.UUID, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "extra"}, post.Revision)
	}))
	apply := models.PostConsolidationReviewApplyInput{PostConsolidationReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
	raw, err := json.Marshal(apply)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/post-consolidation/apply", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(w, r)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "preview_changed")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		review, err := repo.SourceEvidence.ConsolidationReview(ctx, apply.RequestUUID)
		require.NoError(t, err)
		require.Nil(t, review)
		post, err := repo.SourceEvidence.PostIdentity(ctx, input.SourceUUID)
		require.NoError(t, err)
		require.Equal(t, input.SourceUUID, post.CanonicalUUID)
		return nil
	}))
	for _, body := range []string{`{}`, `{"unknown":true}`, `{"source_uuid":"a","source_uuid":"b"}`} {
		r = httptest.NewRequest(http.MethodPost, "/post-consolidation/preview", bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	for _, path := range []string{"/post-consolidation/preview", "/post-consolidation/apply", "/post-consolidation/requests/" + apply.RequestUUID + "/check", "/post-merge-notifications/" + uuid.NewString() + "/retry"} {
		r = httptest.NewRequest(http.MethodPost, path, nil)
		r.Header.Set("Origin", "https://unrelated.example")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	}
}
