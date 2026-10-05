package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) previewPolicyImport(w http.ResponseWriter, r *http.Request) {
	var input models.MetadataPolicyImportInput
	if err := readIngestJSON(w, r, 1<<20, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.MetadataPolicyImportPlan
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MetadataPolicyImport.Preview(ctx, input, time.Now())
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyPolicyImport(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Binding  models.MetadataPolicyImportInput `json:"binding"`
		Expected string                           `json:"expected_plan_sha256"`
	}
	if err := readIngestJSON(w, r, (1<<20)+1024, &request); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.MetadataPolicyImport
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MetadataPolicyImport.Apply(ctx, request.Binding, request.Expected, time.Now())
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) policyImport(w http.ResponseWriter, r *http.Request) {
	var result *models.MetadataPolicyImportDetails
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MetadataPolicyImport.Find(ctx, chi.URLParam(r, "import"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) policyImports(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result []models.MetadataPolicyImport
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MetadataPolicyImport.List(ctx, chi.URLParam(r, "collection"), r.URL.Query().Get("after"), limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
