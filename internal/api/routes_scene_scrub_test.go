package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/session"
	"github.com/stashapp/stash/pkg/signedurl"
	"github.com/stretchr/testify/require"
)

func TestKeyframeRoutesRequireStreamingAndAVideoFile(t *testing.T) {
	scene := &models.Scene{Files: models.NewRelatedVideoFiles(nil)}
	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/scene/s/keyframes", nil)
		return r.WithContext(context.WithValue(r.Context(), sceneKey, scene))
	}
	disabled := sceneRoutes{streamManager: func() *ffmpeg.StreamManager { return nil }}
	w := httptest.NewRecorder()
	disabled.KeyframeIndex(w, request())
	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	streams := ffmpeg.NewStreamManager(t.TempDir(), nil, nil, nil, nil)
	t.Cleanup(streams.Shutdown)
	enabled := sceneRoutes{streamManager: func() *ffmpeg.StreamManager { return streams }}
	for _, handler := range []http.HandlerFunc{enabled.KeyframeIndex, enabled.Keyframe} {
		w := httptest.NewRecorder()
		handler(w, request())
		require.Equal(t, http.StatusNotFound, w.Code)
	}
}

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

// Keyframe previews are renditions of the scene stream: the playback URL's
// stream signature authorizes them for the same scene only, as it does the
// scrub stream. Without it, ordinary library authentication applies.
func TestSignedStreamURLAuthorizesKeyframePreviews(t *testing.T) {
	c := config.NewEmpty()
	c.SetString(config.Username, "owner")
	c.SetPassword("test password")
	c.SetString(config.SessionStoreKey, strings.Repeat("k", 32))
	c.SetString(config.JWTSignKey, "keyframe-signing-key")
	handler := authenticateHandler(c, session.NewStore(c))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	const scene = "0b5cbd36-5c2f-4b2e-9a7f-6f1f3c1d2e4a"
	signed := signedParams(c, "owner", signedurl.DerivePrefix("/scene/"+scene+"/stream")).Encode()
	for _, tc := range []struct {
		path    string
		allowed bool
	}{
		{"/scene/" + scene + "/stream.scrub.master.m3u8?resolution=LOW&" + signed, true},
		{"/scene/" + scene + "/keyframes?" + signed, true},
		{"/scene/" + scene + "/keyframe?t=1.5&v=abc&" + signed, true},
		{"/scene/00000000-0000-4000-8000-000000000000/keyframes?" + signed, false},
		{"/scene/" + scene + "/screenshot?" + signed, false},
		{"/scene/" + scene + "/keyframes", false},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if tc.allowed {
			require.Equal(t, http.StatusNoContent, w.Code, tc.path)
		} else {
			// Unauthenticated page-like paths redirect to the login page.
			require.Contains(t, []int{http.StatusUnauthorized, http.StatusFound}, w.Code, tc.path)
		}
	}
}
