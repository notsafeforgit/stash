package api

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/manager"
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
	return r
}

func nativeCheckpointError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, manager.ErrNativeCheckpointInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, manager.ErrNativeCheckpointBusy), errors.Is(err, manager.ErrNativeCheckpointIncomplete):
		status = http.StatusConflict
	case errors.Is(err, os.ErrNotExist):
		status = http.StatusNotFound
	}
	w.Header().Set("Cache-Control", "no-store")
	ingestJSON(w, status, map[string]string{"error": err.Error()})
}

func (rs *nativeBackupRoutes) capture(w http.ResponseWriter, r *http.Request) {
	var input manager.NativeCheckpointInput
	if err := readIngestJSON(w, r, 256<<10, &input); err != nil {
		nativeCheckpointError(w, manager.ErrNativeCheckpointInvalid)
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
