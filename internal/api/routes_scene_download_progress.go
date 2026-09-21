package api

import (
	"encoding/json"
	"net/http"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
)

func (rs sceneRoutes) DownloadProgress(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	serveDownloadProgress(w, r, manager.GetInstance().StreamManager, scene.ID)
}

func serveDownloadProgress(w http.ResponseWriter, r *http.Request, streams *ffmpeg.StreamManager, sceneID int) {
	w.Header().Set("Cache-Control", "no-store")
	requestID := r.URL.Query().Get("request_id")
	if !ffmpeg.ValidDownloadRequestID(requestID) {
		http.Error(w, "invalid download request ID", http.StatusBadRequest)
		return
	}
	if streams == nil {
		http.Error(w, "Live transcoding disabled", http.StatusServiceUnavailable)
		return
	}
	progress := streams.GetDownloadProgress(sceneID, requestID)
	if progress == nil {
		// The download may not have reached FFmpeg yet, or may be a direct file.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(progress); err != nil {
		logger.Tracef("[download] error writing progress: %v", err)
	}
}
