package api

import (
	"net/http"

	"github.com/stashapp/stash/pkg/models"
)

func (rs *ingestRoutes) requestEnrichmentSource(w http.ResponseWriter, r *http.Request) {
	var input struct {
		enrichmentLeaseInput
		models.SourceRequestInput
	}
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.enrichmentWorker().RequestSource(r.Context(), ingestToken(r), input.lease(r), input.SourceRequestInput)
	writeEnrichmentWorker(w, value, err)
}
func (rs *ingestRoutes) reportEnrichmentSource(w http.ResponseWriter, r *http.Request) {
	var input struct {
		enrichmentLeaseInput
		models.SourceRequestResult
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	err := rs.enrichmentWorker().ReportSourceRequest(r.Context(), ingestToken(r), input.lease(r), input.SourceRequestResult)
	writeEnrichmentWorker(w, map[string]bool{"recorded": true}, err)
}
