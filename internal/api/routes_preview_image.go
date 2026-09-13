package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/previewimage"
)

func (rs sceneRoutes) PreviewImage(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	rs.servePreviewImage(w, r, scene, "cover", manager.GetInstance().ScenePreviewImage(scene))
}

func (rs sceneRoutes) MarkerPreviewImage(w http.ResponseWriter, r *http.Request) {
	scene := r.Context().Value(sceneKey).(*models.Scene)
	id, _ := strconv.Atoi(chi.URLParam(r, "sceneMarkerId"))
	var marker *models.SceneMarker
	if err := rs.withReadTxn(r, func(ctx context.Context) error {
		var err error
		marker, err = rs.sceneMarkerFinder.Find(ctx, id)
		return err
	}); err != nil {
		http.Error(w, "Unable to load marker", http.StatusInternalServerError)
		return
	}
	if marker == nil || marker.SceneID != scene.ID {
		http.NotFound(w, r)
		return
	}
	rs.servePreviewImage(w, r, scene, "marker", manager.GetInstance().MarkerPreviewImage(scene, marker))
}

func (rs sceneRoutes) servePreviewImage(w http.ResponseWriter, r *http.Request, scene *models.Scene, kind string, manifest *previewimage.Manifest) {
	servePreviewImage(w, r, manager.GetInstance().PreviewImageStore(), scene.ID, kind, manifest, chi.URLParam(r, "previewFile"))
}

func servePreviewImage(w http.ResponseWriter, r *http.Request, store previewimage.Store, sceneID int, kind string, manifest *previewimage.Manifest, file string) {
	// Resolve through the current entity before serving cached bytes. A URL
	// cannot resurrect an obsolete cover or another scene's marker artwork.
	if manifest == nil || manifest.Revision != r.URL.Query().Get("revision") {
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}
	path, variant := store.File(sceneID, kind, manifest, file)
	if variant == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", variant.MIMEType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}
