package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestScrubPreviewRequest(t *testing.T) {
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"video?stream_session=preview-owner&resolution=ORIGINAL&start=20&end=30", 200},
		{"audio?stream_session=preview-owner", 404},
		{"video", 400},
		{"video?stream_session=invalid%2Fowner", 400},
	} {
		t.Run(tc.path, func(t *testing.T) {
			router := chi.NewRouter()
			router.Get("/{track}", scrubPreviewRequest(func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, r.ParseForm())
				require.Equal(t, "LOW", r.Form.Get("resolution"))
				require.Equal(t, "20", r.Form.Get("start"))
				require.Empty(t, r.Form.Get("end"))
				require.Equal(t, "preview-owner", r.Form.Get("stream_session"))
				w.WriteHeader(http.StatusOK)
			}))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/"+tc.path, nil))
			require.Equal(t, tc.status, w.Code)
		})
	}
}
