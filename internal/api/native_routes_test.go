package api

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stretchr/testify/require"
)

func TestNativeSceneRoutesAvailableByDefault(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	routes := sceneRoutes{}.Routes()
	for _, path := range []string{
		"/1/preview-image/cover.avif",
		"/1/scene_marker/2/preview-image/marker.avif",
		"/1/stream.master.m3u8",
		"/1/stream.m3u8/video.m3u8",
		"/1/stream.m3u8/video/init.mp4",
		"/1/stream.m3u8/video/0.m4s",
		"/1/stream.fmp4.master.m3u8",
		"/1/stream.fmp4.m3u8/audio.m3u8",
		"/1/stream.fmp4.m3u8/audio/init.mp4",
		"/1/stream.fmp4.m3u8/audio/0.m4s",
		"/1/stream.fmp4.aac.master.m3u8",
		"/1/stream.fmp4.aac.m3u8/audio.m3u8",
		"/1/stream.fmp4.aac.m3u8/audio/init.mp4",
		"/1/stream.fmp4.aac.m3u8/audio/0.m4s",
		"/1/download.mp4",
		"/1/download/progress",
		"/1/screenshot",
	} {
		require.True(t, routes.Match(chi.NewRouteContext(), http.MethodGet, path), path)
	}
	require.True(t, routes.Match(chi.NewRouteContext(), http.MethodHead, "/1/download.mp4"))
	for _, path := range []string{"/1/streams.stop", "/1/streams.keepalive"} {
		require.True(t, routes.Match(chi.NewRouteContext(), http.MethodPost, path), path)
	}
}
