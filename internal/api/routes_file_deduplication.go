package api

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/dedup"
	"github.com/stashapp/stash/pkg/models"
)

func fileDeduplicationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrFileDeduplicationInvalid):
		ingestError(w, ingest.ErrInvalid)
	case errors.Is(err, models.ErrFileDeduplicationReplay):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "deduplication_request_changed"})
	case errors.Is(err, models.ErrFileDeduplicationConflict), errors.Is(err, models.ErrFileGenerationConflict),
		errors.Is(err, models.ErrArchiveIdentityConflict), errors.Is(err, models.ErrFileContentConflict), errors.Is(err, archive.ErrMediaFileChanged):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "deduplication_preview_changed"})
	case errors.Is(err, archive.ErrMediaFileDigest):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "deduplication_bytes_differ"})
	case errors.Is(err, os.ErrNotExist):
		ingestJSON(w, http.StatusNotFound, map[string]string{"error": "file_not_found"})
	default:
		ingestError(w, err)
	}
}

func (rs *nativeArchiveRoutes) previewFileDeduplication(w http.ResponseWriter, r *http.Request) {
	var input models.FileDeduplicationInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		fileDeduplicationError(w, err)
		return
	}
	result, err := dedup.New(rs.repo, "").Preview(r.Context(), input)
	if err != nil {
		fileDeduplicationError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyFileDeduplication(w http.ResponseWriter, r *http.Request) {
	var input dedup.Request
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		fileDeduplicationError(w, err)
		return
	}
	result, err := dedup.New(rs.repo, config.GetInstance().GetDeleteTrashPath()).Apply(r.Context(), input)
	if err != nil {
		fileDeduplicationError(w, err)
		return
	}
	// The proof remains in the archive. Operational callers need the committed
	// identity and result, not absolute host paths or per-file technical metadata.
	result.Proof = nil
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) fileDeduplicationRequest(w http.ResponseWriter, r *http.Request) {
	var result *models.FileDeduplicationReceipt
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.FileDeduplication.Find(ctx, chi.URLParam(r, "request"))
		return err
	})
	if err == nil && result == nil {
		err = ingest.ErrNotFound
	}
	if err != nil {
		fileDeduplicationError(w, err)
		return
	}
	result.Proof = nil
	ingestJSON(w, http.StatusOK, result)
}
