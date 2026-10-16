package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/models"
)

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
