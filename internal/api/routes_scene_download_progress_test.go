package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stretchr/testify/require"
)

func TestSceneDownloadProgressUnknownAndInvalidRequests(t *testing.T) {
	for _, input := range []struct {
		query   string
		streams *ffmpeg.StreamManager
		want    int
	}{
		{"", &ffmpeg.StreamManager{}, http.StatusBadRequest},
		{"?request_id=..%2Fpath", &ffmpeg.StreamManager{}, http.StatusBadRequest},
		{"?request_id=request", nil, http.StatusServiceUnavailable},
		{"?request_id=request", &ffmpeg.StreamManager{}, http.StatusNoContent},
	} {
		req := httptest.NewRequest(http.MethodGet, "/scene/1/download/progress"+input.query, nil)
		w := httptest.NewRecorder()
		serveDownloadProgress(w, req, input.streams, 1)
		require.Equal(t, input.want, w.Code)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
}
