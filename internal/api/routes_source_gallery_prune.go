package api

import (
	"context"
	"net/http"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) pruneSourceGalleries(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Galleries []models.SourceGalleryPruneInput `json:"galleries"`
	}
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	if len(input.Galleries) == 0 || len(input.Galleries) > 100 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	seen := make(map[string]bool, len(input.Galleries))
	for _, g := range input.Galleries {
		if !ingest.ValidUUID(g.GalleryUUID) || g.Revision < 1 || seen[g.GalleryUUID] {
			ingestError(w, ingest.ErrInvalid)
			return
		}
		seen[g.GalleryUUID] = true
	}
	var results []models.SourceGalleryPruneResult
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		results, err = rs.repo.SourceGallery.PruneSmall(ctx, input.Galleries)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"results": results})
}
