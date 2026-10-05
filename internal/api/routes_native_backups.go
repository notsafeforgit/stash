package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/logger"
)

type nativeBackupRoutes struct{ manager *manager.Manager }

// Mounted behind application authentication, never producer-token routing.
// Checkpoint artifacts include private settings and require the same authority
// as database backup. The manifest response contains no settings values.
func (rs *nativeBackupRoutes) router() http.Handler {
	r := chi.NewRouter()
	r.Use(nativeAdminOrigin)
	r.Post("/checkpoints", rs.capture)
	r.Get("/checkpoints/{checkpoint}", rs.read)
	r.Get("/checkpoints/{checkpoint}/components/{component}", rs.component)
	r.Post("/checkpoints/{checkpoint}/release", rs.release)
	r.Get("/checkpoints/{checkpoint}/release", rs.released)
	r.Get("/checkpoints/{checkpoint}/status", rs.status)
	r.Post("/checkpoints/{checkpoint}/abandon", rs.abandon)
	r.Post("/checkpoints/{checkpoint}/boundary", rs.boundary)
	return r
}

func nativeCheckpointError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, manager.ErrNativeCheckpointInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, manager.ErrNativeCheckpointBusy), errors.Is(err, manager.ErrNativeCheckpointIncomplete), errors.Is(err, manager.ErrNativeCheckpointSealed):
		status = http.StatusConflict
	case errors.Is(err, manager.ErrNativeCheckpointReleased), errors.Is(err, manager.ErrNativeCheckpointAbandoned):
		status = http.StatusGone
	case errors.Is(err, manager.ErrNativeCheckpointBoundaryExpired):
		status = http.StatusGone
	case errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
	case errors.Is(err, os.ErrNotExist):
		status = http.StatusNotFound
	}
	w.Header().Set("Cache-Control", "no-store")
	ingestJSON(w, status, map[string]string{"error": err.Error()})
}

func (rs *nativeBackupRoutes) status(w http.ResponseWriter, r *http.Request) {
	result, err := rs.manager.NativeCheckpointState(r.Context(), chi.URLParam(r, "checkpoint"))
	if err != nil {
		nativeCheckpointError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeBackupRoutes) abandon(w http.ResponseWriter, r *http.Request) {
	var input manager.NativeCheckpointAbandonInput
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		nativeCheckpointError(w, manager.ErrNativeCheckpointInvalid)
		return
	}
	result, err := rs.manager.AbandonNativeCheckpoint(r.Context(), chi.URLParam(r, "checkpoint"), input)
	if err != nil {
		nativeCheckpointError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeBackupRoutes) release(w http.ResponseWriter, r *http.Request) {
	var input manager.NativeCheckpointReleaseInput
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		nativeCheckpointError(w, manager.ErrNativeCheckpointInvalid)
		return
	}
	result, err := rs.manager.ReleaseNativeCheckpoint(r.Context(), chi.URLParam(r, "checkpoint"), input)
	if err != nil {
		nativeCheckpointError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeBackupRoutes) released(w http.ResponseWriter, r *http.Request) {
	result, err := rs.manager.ReadNativeCheckpointRelease(chi.URLParam(r, "checkpoint"))
	if err != nil {
		nativeCheckpointError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeBackupRoutes) capture(w http.ResponseWriter, r *http.Request) {
	var input manager.NativeCheckpointInput
	if err := readIngestJSON(w, r, 256<<10, &input); err != nil {
		nativeCheckpointError(w, manager.ErrNativeCheckpointInvalid)
		return
	}
	if input.ExternalBoundary != nil {
		rs.captureWithBoundary(w, r, input)
		return
	}
	result, err := rs.manager.CaptureNativeCheckpoint(r.Context(), input)
	if err != nil {
		nativeCheckpointError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeBackupRoutes) captureWithBoundary(w http.ResponseWriter, r *http.Request, input manager.NativeCheckpointInput) {
	streaming := false
	result, err := rs.manager.CaptureNativeCheckpointWithBoundary(r.Context(), input, func(ready manager.NativeCheckpointBoundaryReady) error {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("X-Accel-Buffering", "no")
		streaming = true
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"event": "boundary_ready", "ready": ready}); err != nil {
			return err
		}
		return http.NewResponseController(w).Flush()
	})
	if !streaming {
		// An identical sealed replay does not ask for a second filesystem view.
		if err != nil {
			nativeCheckpointError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		ingestJSON(w, http.StatusOK, result)
		return
	}
	if err != nil {
		if writeErr := json.NewEncoder(w).Encode(map[string]string{"event": "error", "error": err.Error()}); writeErr != nil {
			logger.Debugf("Native checkpoint response ended: %v", writeErr)
		}
		return
	}
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"event": "sealed", "checkpoint": result}); err != nil {
		logger.Debugf("Native checkpoint response ended: %v", err)
	}
}

func (rs *nativeBackupRoutes) boundary(w http.ResponseWriter, r *http.Request) {
	var input manager.NativeCheckpointBoundaryConfirmation
	if err := readIngestJSON(w, r, 128<<10, &input); err != nil {
		nativeCheckpointError(w, manager.ErrNativeCheckpointInvalid)
		return
	}
	result, err := rs.manager.ConfirmNativeCheckpointBoundary(r.Context(), chi.URLParam(r, "checkpoint"), input)
	if err != nil {
		nativeCheckpointError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeBackupRoutes) read(w http.ResponseWriter, r *http.Request) {
	result, err := rs.manager.ReadNativeCheckpoint(chi.URLParam(r, "checkpoint"))
	if err != nil {
		nativeCheckpointError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeBackupRoutes) component(w http.ResponseWriter, r *http.Request) {
	f, component, err := rs.manager.OpenNativeCheckpointComponent(chi.URLParam(r, "checkpoint"), chi.URLParam(r, "component"))
	if err != nil {
		nativeCheckpointError(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Stash-SHA256", component.SHA256)
	w.Header().Set("ETag", `"`+component.SHA256+`"`)
	http.ServeContent(w, r, component.Name, time.Time{}, f)
}
