package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

// Reviewed aliases use the existing immutable evidence and revision checks.
// This application-authenticated route cannot claim to be a source capture.
func (rs *nativeArchiveRoutes) reviewPostIdentifier(w http.ResponseWriter, r *http.Request) {
	var input models.SourcePostIdentifierInput
	if err := readIngestJSON(w, r, (64<<10)+4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	if input.PostUUID != chi.URLParam(r, "post") || input.Origin != "review" {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.SourcePostIdentifierObservation
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourcePostLinks.ObserveIdentifier(ctx, input)
		return err
	})
	switch {
	case err == nil:
		ingestJSON(w, http.StatusOK, result)
	case errors.Is(err, models.ErrSourcePostEvidenceInvalid):
		ingestError(w, ingest.ErrInvalid)
	case errors.Is(err, models.ErrSourcePostEvidenceReplay), errors.Is(err, models.ErrSourcePostConflict), errors.Is(err, models.ErrSourcePostForgotten):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "post_identifier_conflict", "message": "The post, identifier or saved review changed. Refresh its identity and review evidence."})
	default:
		nativeArchiveError(w, err)
	}
}
