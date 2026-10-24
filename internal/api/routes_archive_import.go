package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func importHistoryError(w http.ResponseWriter, err error) {
	if errors.Is(err, models.ErrImportPayloadRetentionConflict) {
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "import_payloads_retained_or_checkpoint_changed"})
		return
	}
	if errors.Is(err, models.ErrArchiveImportInvalid) {
		ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_import_history_request"})
		return
	}
	ingestError(w, err)
}

func (rs *nativeArchiveRoutes) importPayloadRetention(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		importHistoryError(w, models.ErrArchiveImportInvalid)
		return
	}
	var result *models.ImportPayloadRetention
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.ArchiveImport.PayloadRetention(ctx, chi.URLParam(r, "kind"), chi.URLParam(r, "snapshot"))
		return err
	})
	if err != nil {
		importHistoryError(w, err)
		return
	}
	if result == nil {
		ingestError(w, ingest.ErrNotFound)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) retireImportPayloads(w http.ResponseWriter, r *http.Request) {
	if chi.URLParam(r, "kind") != "catalog" || len(r.URL.Query()) != 0 {
		importHistoryError(w, models.ErrArchiveImportInvalid)
		return
	}
	var input struct {
		After *int64 `json:"after"`
		Limit int    `json:"limit"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	if input.After == nil {
		importHistoryError(w, models.ErrArchiveImportInvalid)
		return
	}
	var result *models.ImportPayloadRetirement
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.ArchiveImport.RetirePayloads(ctx, chi.URLParam(r, "snapshot"), *input.After, input.Limit, time.Now())
		return err
	})
	if err != nil {
		importHistoryError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) importSnapshots(w http.ResponseWriter, r *http.Request) {
	filter := models.ArchiveImportFilter{Kind: chi.URLParam(r, "kind"), After: r.URL.Query().Get("after"), Limit: 25}
	for key, values := range r.URL.Query() {
		if (key != "after" && key != "limit") || len(values) != 1 || values[0] == "" {
			importHistoryError(w, models.ErrArchiveImportInvalid)
			return
		}
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		filter.Limit, err = strconv.Atoi(value)
		if err != nil {
			importHistoryError(w, models.ErrArchiveImportInvalid)
			return
		}
	}
	var rows []models.ArchiveImportSnapshot
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		rows, err = rs.repo.ArchiveImport.Snapshots(ctx, filter)
		return err
	})
	if err != nil {
		importHistoryError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, rows)
}

func (rs *nativeArchiveRoutes) importSnapshot(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		importHistoryError(w, models.ErrArchiveImportInvalid)
		return
	}
	var row *models.ArchiveImportDetails
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		row, err = rs.repo.ArchiveImport.Snapshot(ctx, chi.URLParam(r, "kind"), chi.URLParam(r, "snapshot"))
		return err
	})
	if err != nil {
		importHistoryError(w, err)
		return
	}
	if row == nil {
		ingestError(w, ingest.ErrNotFound)
		return
	}
	ingestJSON(w, http.StatusOK, row)
}
