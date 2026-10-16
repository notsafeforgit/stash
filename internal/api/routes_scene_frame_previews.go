package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/previewimage"
)

func (rs sceneRoutes) FramePreviews(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	scene := r.Context().Value(sceneKey).(*models.Scene)
	file := scene.Files.Primary()
	if file == nil {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	start, startErr := strconv.ParseFloat(q.Get("start"), 64)
	end, endErr := strconv.ParseFloat(q.Get("end"), 64)
	mode := q.Get("mode")
	req := previewimage.PickerRequest{Start: start, End: end, Frames: mode == "frames"}
	if startErr != nil || endErr != nil || (mode != "samples" && mode != "frames") || !req.Valid(file.Duration) {
		http.Error(w, "Invalid thumbnail preview range", http.StatusBadRequest)
		return
	}
	frames, err := manager.GetInstance().SceneFramePreviews(r.Context(), file, req)
	if err != nil {
		if errors.Is(err, manager.ErrFramePreviewsBusy) {
			w.Header().Set("Retry-After", "2")
			http.Error(w, "Thumbnail previews are busy. Please retry.", http.StatusTooManyRequests)
		} else if r.Context().Err() == nil {
			logger.Warnf("scene %d thumbnail preview: %v", scene.ID, err)
			http.Error(w, "Unable to extract thumbnail previews. Try another position.", http.StatusUnprocessableEntity)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(frames); err != nil && r.Context().Err() == nil {
		logger.Warnf("writing scene %d thumbnail previews: %v", scene.ID, err)
	}
}
