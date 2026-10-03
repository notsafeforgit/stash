package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) previewEnrichmentActivation(w http.ResponseWriter, r *http.Request) {
	var input models.EnrichmentActivationInput
	if err := readIngestJSON(w, r, archive.MaxEnrichmentActivationBytes, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.EnrichmentActivationPlan
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.EnrichmentWork.PreviewActivation(ctx, input)
		return err
	})
	writeEnrichmentWork(w, result, err)
}

func (rs *nativeArchiveRoutes) activateEnrichments(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Input    models.EnrichmentActivationInput `json:"input"`
		Expected string                           `json:"expected_plan_sha256"`
	}
	if err := readIngestJSON(w, r, archive.MaxEnrichmentActivationBytes, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.EnrichmentActivation
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.EnrichmentWork.Activate(ctx, input.Input, input.Expected, time.Now())
		return err
	})
	writeEnrichmentWork(w, result, err)
}

func (rs *nativeArchiveRoutes) enrichmentActivation(w http.ResponseWriter, r *http.Request) {
	var result *models.EnrichmentActivation
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.EnrichmentWork.Activation(ctx, chi.URLParam(r, "activation"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	writeEnrichmentWork(w, result, err)
}

func (rs *nativeArchiveRoutes) heldAutomationEnrichments(w http.ResponseWriter, r *http.Request) {
	after, limit := int64(0), archive.MaxEnrichmentActivationTargets
	var err error
	if value := r.URL.Query().Get("after"); value != "" {
		after, err = strconv.ParseInt(value, 10, 64)
	}
	if err == nil {
		if value := r.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
		}
	}
	if err != nil {
		writeEnrichmentWork(w, nil, models.ErrEnrichmentInvalid)
		return
	}
	var result []models.AutomationEnrichmentCandidate
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.AutomationEnrichmentImport.HeldTargets(ctx, chi.URLParam(r, "snapshot"), r.URL.Query().Get("expected_manifest_sha256"), after, limit)
		return err
	})
	writeEnrichmentWork(w, result, err)
}
