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

func (rs *nativeArchiveRoutes) previewCatalogRegistryImport(w http.ResponseWriter, r *http.Request) {
	var input models.CatalogRegistryImportInput
	if err := readIngestJSON(w, r, 17<<20, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.CatalogRegistryImportPlan
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogRegistryImport.Preview(ctx, input, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyCatalogRegistryImport(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Binding  models.CatalogRegistryImportInput `json:"binding"`
		Expected string                            `json:"expected_plan_sha256"`
	}
	if err := readIngestJSON(w, r, 17<<20, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.CatalogRegistryImport
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogRegistryImport.Apply(ctx, input.Binding, input.Expected, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) catalogRegistryImport(w http.ResponseWriter, r *http.Request) {
	var result *models.CatalogRegistryImport
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogRegistryImport.Find(ctx, chi.URLParam(r, "import"))
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

func (rs *nativeArchiveRoutes) catalogRegistryImportRecords(w http.ResponseWriter, r *http.Request) {
	after := int64(0)
	if value := r.URL.Query().Get("after"); value != "" {
		var err error
		after, err = strconv.ParseInt(value, 10, 64)
		if err != nil || after < 0 {
			ingestError(w, models.ErrCatalogRegistryImportInvalid)
			return
		}
	}
	var result []models.CatalogRegistryImportRecord
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogRegistryImport.Records(ctx, chi.URLParam(r, "import"), after, 100)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
