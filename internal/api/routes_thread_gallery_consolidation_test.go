package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestThreadGalleryConsolidationHTTPRequiresSourceTrackingAndAThread(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	post := albumHTTPPost(t, repo)
	path := "/posts/" + post.UUID + "/thread-album-consolidation"
	require.Contains(t, get(path, http.StatusConflict).Body.String(), "album_preview_changed", "a post outside a thread has nothing to combine")
	get("/posts/"+uuid.NewString()+"/thread-album-consolidation", http.StatusNotFound)
	get("/posts/not-a-post/thread-album-consolidation", http.StatusNotFound)

	routes := &nativeArchiveRoutes{repo: repo}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{"signature":"stale"}`)))
	r.Header.Set("Content-Type", "application/json")
	routes.router().ServeHTTP(w, r)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, bytes.NewReader([]byte(`{"signature":"stale"}`)))
		r.Header.Set("Content-Type", "application/json")
		routes.routerWithSourceTracking(false).ServeHTTP(w, r)
		require.NotEqual(t, http.StatusOK, w.Code, "installations without source tracking do not expose thread albums")
		require.NotEqual(t, http.StatusConflict, w.Code)
	}
}
