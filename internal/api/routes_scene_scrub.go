package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/models"
)

// KeyframeIndex lists the primary file's video keyframes in scene time.
func (rs sceneRoutes) KeyframeIndex(w http.ResponseWriter, r *http.Request) {
	rs.serveKeyframes(w, r, (*ffmpeg.StreamManager).ServeKeyframeIndex)
}

// Keyframe returns a scrub-sized JPEG of the keyframe at or before ?t=.
func (rs sceneRoutes) Keyframe(w http.ResponseWriter, r *http.Request) {
	rs.serveKeyframes(w, r, (*ffmpeg.StreamManager).ServeKeyframe)
}

func (rs sceneRoutes) serveKeyframes(w http.ResponseWriter, r *http.Request, serve func(*ffmpeg.StreamManager, http.ResponseWriter, *http.Request, *models.VideoFile)) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	streamManager := rs.streamManager()
	if streamManager == nil {
		http.Error(w, "keyframe previews unavailable", http.StatusServiceUnavailable)
		return
	}
	f := scene.Files.Primary()
	if f == nil {
		http.Error(w, "scene has no video file", http.StatusNotFound)
		return
	}
	serve(streamManager, w, r, f)
}

// The preview shares ordinary scene authorization, but never the main player's
// transcode owner. Fixed resolution and video-only tracks bound its work.
func scrubPreviewRequest(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := ffmpeg.V3StreamSessionFromRequest(r)
		if err != nil || session == "" {
			http.Error(w, "scrub preview requires a stream session", http.StatusBadRequest)
			return
		}
		if track := chi.URLParam(r, "track"); track != "" && track != "video" {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		query.Set("resolution", models.StreamingResolutionEnumLow.String())
		// A scrub preview uses absolute scene time even for marker players.
		query.Del("end")
		r.URL.RawQuery = query.Encode()
		r.Form = nil
		next(w, r)
	}
}
