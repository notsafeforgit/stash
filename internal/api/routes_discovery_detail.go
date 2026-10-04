package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) previewDiscoveryDetail(w http.ResponseWriter, r *http.Request) {
	var input models.DiscoveryDetailPreviewInput
	if err := readIngestJSON(w, r, archive.MaxEnrichmentTranscriptBytes+4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	input.TargetUUID = chi.URLParam(r, "target")
	var result *models.DiscoveryDetailPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryMatch.PreviewDetail(ctx, input)
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	writeDiscoveryWorker(w, result, err)
}
