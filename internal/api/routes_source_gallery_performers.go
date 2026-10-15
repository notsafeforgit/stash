package api

import (
	"context"
	"net/http"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) reconcileSourceGalleryPerformers(w http.ResponseWriter, r *http.Request) {
	rs.reconcileSourceGalleryAttribution(w, r, false)
}

func (rs *nativeArchiveRoutes) reconcileSourceGalleryStudios(w http.ResponseWriter, r *http.Request) {
	rs.reconcileSourceGalleryAttribution(w, r, true)
}

func (rs *nativeArchiveRoutes) reconcileSourceGalleryAttribution(w http.ResponseWriter, r *http.Request, studios bool) {
	var input struct {
		GalleryUUIDs []string `json:"gallery_uuids"`
	}
	if err := readIngestJSON(w, r, 8192, &input); err != nil {
		ingestError(w, err)
		return
	}
	if len(input.GalleryUUIDs) == 0 || len(input.GalleryUUIDs) > 100 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	seen := make(map[string]bool, len(input.GalleryUUIDs))
	for _, id := range input.GalleryUUIDs {
		if !ingest.ValidUUID(id) || seen[id] {
			ingestError(w, ingest.ErrInvalid)
			return
		}
		seen[id] = true
	}
	var results []models.SourceGalleryAttributionResult
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		field := "performer_ids"
		if studios {
			field = "studio_id"
			results, err = rs.repo.SourceGallery.ReconcileStudios(ctx, input.GalleryUUIDs)
		} else {
			results, err = rs.repo.SourceGallery.ReconcilePerformers(ctx, input.GalleryUUIDs)
		}
		if err != nil {
			return err
		}
		if rs.notifyMetadata != nil {
			for _, result := range results {
				if result.Status == "updated" {
					if err := rs.notifyMetadata(ctx, metadata.Input{EntityUUID: result.GalleryUUID}, []string{field}); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"results": results})
}
