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

func (rs *nativeArchiveRoutes) previewCatalogIdentityImport(w http.ResponseWriter, r *http.Request) {
	var input models.CatalogIdentityImportInput
	if err := readIngestJSON(w, r, 9<<20, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.CatalogIdentityImportPlan
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogIdentityImport.Preview(ctx, input, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyCatalogIdentityImport(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Binding  models.CatalogIdentityImportInput `json:"binding"`
		Expected string                            `json:"expected_plan_sha256"`
	}
	if err := readIngestJSON(w, r, 9<<20, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.CatalogIdentityImport
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogIdentityImport.Apply(ctx, input.Binding, input.Expected, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) catalogIdentityImport(w http.ResponseWriter, r *http.Request) {
	var result *models.CatalogIdentityImport
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogIdentityImport.Find(ctx, chi.URLParam(r, "import"))
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

func (rs *nativeArchiveRoutes) catalogIdentityImportRecords(w http.ResponseWriter, r *http.Request) {
	after := int64(0)
	if value := r.URL.Query().Get("after"); value != "" {
		var err error
		after, err = strconv.ParseInt(value, 10, 64)
		if err != nil || after < 0 {
			ingestError(w, models.ErrCatalogIdentityImportInvalid)
			return
		}
	}
	var result []models.CatalogIdentityImportRecord
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogIdentityImport.Records(ctx, chi.URLParam(r, "import"), after, 100)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
