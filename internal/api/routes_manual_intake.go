package api

import (
	"errors"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func manualIntakeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, archive.ErrMediaDirectoryInput):
		ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_directory_request"})
	case errors.Is(err, archive.ErrMediaDirectoryChanged):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "directory_changed"})
	case errors.Is(err, archive.ErrMediaDirectoryLimit):
		ingestJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "directory_review_limit"})
	case errors.Is(err, ingest.ErrManualFileChanged), errors.Is(err, ingest.ErrDefinition), errors.Is(err, models.ErrMetadataPolicyConflict),
		errors.Is(err, models.ErrFilePathChanged), errors.Is(err, models.ErrFileGenerationConflict), errors.Is(err, archive.ErrMediaFileChanged):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "intake_preview_changed"})
	case errors.Is(err, models.ErrArchiveJobConflict):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "intake_request_changed"})
	case errors.Is(err, os.ErrNotExist):
		ingestJSON(w, http.StatusNotFound, map[string]string{"error": "file_not_found"})
	default:
		ingestError(w, err)
	}
}

func (rs *nativeArchiveRoutes) manualIntakeCapabilities(w http.ResponseWriter, _ *http.Request) {
	ingestJSON(w, http.StatusOK, map[string]bool{"file_ingestion": rs.fileIngestion})
}

func (rs *nativeArchiveRoutes) previewManualIntake(w http.ResponseWriter, r *http.Request) {
	var input ingest.ManualFileInput
	if err := readIngestJSON(w, r, 8192, &input); err != nil {
		manualIntakeError(w, err)
		return
	}
	result, err := ingest.New(rs.repo).PreviewManualFile(r.Context(), input)
	if err != nil {
		manualIntakeError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyManualIntake(w http.ResponseWriter, r *http.Request) {
	var input ingest.ManualFileRequest
	if err := readIngestJSON(w, r, 8192, &input); err != nil {
		manualIntakeError(w, err)
		return
	}
	service := ingest.New(rs.repo)
	var result *ingest.ManualFileStatus
	var err error
	if rs.fileIngestion {
		result, err = service.SubmitManualFile(r.Context(), input)
	} else {
		// A temporarily unavailable worker must not prevent recovery of an
		// already accepted request, or accept new work it cannot process.
		result, err = service.ManualFileStatus(r.Context(), input.RequestUUID)
		if errors.Is(err, ingest.ErrNotFound) {
			ingestJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "file_ingestion_unavailable"})
			return
		}
		if err == nil && (result.ResumeFromJobUUID != "" || result.ManualFileInput != input.ManualFileInput || result.Signature != input.Signature) {
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

func (rs *nativeArchiveRoutes) manualIntakeRequest(w http.ResponseWriter, r *http.Request) {
	result, err := ingest.New(rs.repo).ManualFileStatus(r.Context(), chi.URLParam(r, "request"))
	if err != nil {
		manualIntakeError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) cancelManualIntake(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision int64 `json:"expected_revision"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		manualIntakeError(w, err)
		return
	}
	result, err := ingest.New(rs.repo).CancelManualFile(r.Context(), chi.URLParam(r, "request"), input.Revision)
	if err != nil {
		manualIntakeError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) retryManualIntake(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RequestUUID string `json:"request_uuid"`
		Revision    int64  `json:"expected_revision"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		manualIntakeError(w, err)
		return
	}
	service := ingest.New(rs.repo)
	parentRequest := chi.URLParam(r, "request")
	if !ingest.ValidUUID(parentRequest) || !ingest.ValidUUID(input.RequestUUID) || input.Revision < 1 || parentRequest == input.RequestUUID {
		manualIntakeError(w, ingest.ErrInvalid)
		return
	}
	var result *ingest.ManualFileStatus
	var err error
	if rs.fileIngestion {
		result, err = service.RetryManualFile(r.Context(), parentRequest, input.Revision, input.RequestUUID)
	} else {
		result, err = service.ManualFileStatus(r.Context(), input.RequestUUID)
		if errors.Is(err, ingest.ErrNotFound) {
			ingestJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "file_ingestion_unavailable"})
			return
		}
		if err == nil {
			parent, parentErr := service.ManualFileStatus(r.Context(), parentRequest)
			if parentErr != nil {
				err = parentErr
			} else if result.ResumeFromJobUUID != parent.JobUUID || result.ResumeFromRevision != input.Revision {
				err = models.ErrArchiveJobConflict
			}
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
