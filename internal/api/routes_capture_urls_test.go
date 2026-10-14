package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCaptureURLBackfillHTTPRepairsRetainedPostAndPreservesExistingLinks(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	var post *models.SourcePost
	var capture *models.SourceCapture
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "")
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", []byte(`{"category":"reddit","id":"abc123","permalink":"/r/test/comments/abc123/title/"}`))
		if err != nil {
			return err
		}
		capture, err = repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID,
			Origin: "gallery-dl", Platform: "reddit", CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/capture-urls/backfill", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := `{"capture_uuids":["` + capture.UUID + `"]}`
	require.Equal(t, http.StatusForbidden, request(body, "https://unrelated.test").Code)
	for _, invalid := range []string{`{}`, `{"capture_uuids":["invalid"]}`, `{"capture_uuids":["` + capture.UUID + `","` + capture.UUID + `"]}`, `{"capture_uuids":[],"url":"https://example.test"}`} {
		require.Equal(t, http.StatusBadRequest, request(invalid, "").Code)
	}
	for _, status := range []string{"recovered", "preserved"} {
		w := request(body, "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"status":"`+status+`"`)
	}
	var summary models.SourcePostSummary
	require.NoError(t, json.Unmarshal(get("/posts/"+post.UUID, 200).Body.Bytes(), &summary))
	require.Len(t, summary.URLs, 1)
	require.Equal(t, "https://www.reddit.com/r/test/comments/abc123/title/", summary.URLs[0].URL)
	// The admin repair remains unavailable to installations without source tracking.
	w := httptest.NewRecorder()
	(&nativeArchiveRoutes{repo: repo}).routerWithSourceTracking(false).ServeHTTP(w,
		httptest.NewRequest(http.MethodPost, "/capture-urls/backfill", strings.NewReader(body)))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "source_tracking_disabled")
}
