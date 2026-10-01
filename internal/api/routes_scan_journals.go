package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) importScanJournal(w http.ResponseWriter, r *http.Request) {
	var input models.ScanJournalInput
	if err := readIngestJSON(w, r, (8<<20)+1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.ScanJournal
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.ScanJournal.Import(ctx, input, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) scanJournal(w http.ResponseWriter, r *http.Request) {
	var result *models.ScanJournal
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.ScanJournal.Find(ctx, chi.URLParam(r, "journal"))
		return err
	})
	if err == nil && result == nil {
		err = ingest.ErrNotFound
	}
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) scanJournalRecords(w http.ResponseWriter, r *http.Request) {
	after := int64(0)
	if value := r.URL.Query().Get("after"); value != "" {
		var err error
		after, err = strconv.ParseInt(value, 10, 64)
		if err != nil || after < 0 {
			ingestError(w, models.ErrScanJournalInvalid)
			return
		}
	}
	var result []models.ScanJournalRecord
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.ScanJournal.Records(ctx, chi.URLParam(r, "journal"), r.URL.Query().Get("table"), after, 100)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) scanJournalRecord(w http.ResponseWriter, r *http.Request) {
	var result *models.ScanJournalRecord
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.ScanJournal.Record(ctx, chi.URLParam(r, "record"))
		return err
	})
	if err == nil && result == nil {
		err = ingest.ErrNotFound
	}
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
