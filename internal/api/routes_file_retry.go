package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) fileRetryRequest(w http.ResponseWriter, r *http.Request) {
	result, err := ingest.New(rs.repo).FileRetryStatus(r.Context(), chi.URLParam(r, "request"))
	if err != nil {
		manualIntakeError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) retryFile(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RequestUUID string `json:"request_uuid"`
		Revision    int64  `json:"expected_revision"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		manualIntakeError(w, err)
		return
	}
	parent := chi.URLParam(r, "job")
	if !ingest.ValidUUID(parent) || !ingest.ValidUUID(input.RequestUUID) || parent == input.RequestUUID || input.Revision < 1 {
		manualIntakeError(w, ingest.ErrInvalid)
		return
	}
	service := ingest.New(rs.repo)
	var result *ingest.FileRetryStatus
	var err error
	if rs.fileIngestion {
		result, err = service.RetryFile(r.Context(), parent, input.Revision, input.RequestUUID)
	} else {
		result, err = service.FileRetryStatus(r.Context(), input.RequestUUID)
		if errors.Is(err, ingest.ErrNotFound) {
			ingestJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "file_ingestion_unavailable"})
			return
		}
		if err == nil && (result.FromJobUUID != parent || result.FromRevision != input.Revision) {
			err = models.ErrArchiveJobConflict
		}
	}
	if err != nil {
		manualIntakeError(w, err)
		return
	}
	code := http.StatusOK
	if result.State == "queued" || result.State == "running" {
		code = http.StatusAccepted
	}
	ingestJSON(w, code, result)
}
