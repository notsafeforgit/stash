package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchivePaginationHTTP(t *testing.T) {
	_, repo, _ := nativeMetadataReviewFixture(t)
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	for _, tc := range []struct {
		query  string
		status int
	}{
		{"resource=media-roots", 200}, {"resource=review-metadata", 200},
		{"resource=posts&url=https%3A%2F%2Fexample.com%2Fpost", 200},
		{"resource=posts&cursor=invalid", 400}, {"resource=jobs&cursor=-1", 400},
		{"resource=collections&q=one&q=two", 400}, {"resource=secrets", 400},
		{"resource=media-roots&sql=SELECT", 400}, {"resource=media-roots&state=invalid", 400},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/page-info?"+tc.query, nil))
		require.Equal(t, tc.status, w.Code, w.Body.String())
		if tc.status == 200 {
			require.Contains(t, w.Body.String(), `"total":`)
			require.Contains(t, w.Body.String(), `"offset":`)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/page-info?resource=media-roots", nil))
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
