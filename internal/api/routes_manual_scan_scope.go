package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
)

func (rs *nativeArchiveRoutes) manualScanScope(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	for key, values := range query {
		if key != "directory" || len(values) != 1 {
			manualIntakeError(w, ingest.ErrInvalid)
			return
		}
	}
	result, err := ingest.New(rs.repo).ManualScanScope(r.Context(), chi.URLParam(r, "collection"), query.Get("directory"))
	if err != nil {
		manualIntakeError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
