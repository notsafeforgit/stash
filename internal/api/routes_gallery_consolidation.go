package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) previewGalleryConsolidation(w http.ResponseWriter, r *http.Request) {
	var ret *models.GalleryConsolidationPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourceGallery.PreviewConsolidation(ctx, chi.URLParam(r, "entity"))
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}

func (rs *nativeArchiveRoutes) applyGalleryConsolidation(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Signature string `json:"signature"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	var ret *models.GalleryConsolidationResult
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourceGallery.Consolidate(ctx, chi.URLParam(r, "entity"), input.Signature)
		if err == nil && len(ret.MergedUUIDs) > 0 && rs.notifyMetadata != nil {
			err = rs.notifyMetadata(ctx, metadata.Input{EntityUUID: ret.GalleryUUID}, []string{"image_ids", "scene_ids", "alternate_titles", "performer_ids", "tag_ids", "urls"})
		}
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}
