package api

import (
	"context"
	"net/http"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) reconcileSourceGalleryPerformers(w http.ResponseWriter, r *http.Request) {
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
	var results []models.SourceGalleryPerformerResult
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		results, err = rs.repo.SourceGallery.ReconcilePerformers(ctx, input.GalleryUUIDs)
		if err != nil {
			return err
		}
		if rs.notifyMetadata != nil {
			for _, result := range results {
				if result.Status == "updated" {
					if err := rs.notifyMetadata(ctx, metadata.Input{EntityUUID: result.GalleryUUID}, []string{"performer_ids"}); err != nil {
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
