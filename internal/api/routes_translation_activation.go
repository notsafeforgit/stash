package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) previewTranslationActivation(w http.ResponseWriter, r *http.Request) {
	var input models.TranslationActivationInput
	if err := readIngestJSON(w, r, archive.MaxTranslationActivationBytes, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.TranslationActivationPlan
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.TranslationWork.PreviewActivation(ctx, input)
		return err
	})
	writeTranslationWork(w, result, err)
}

func (rs *nativeArchiveRoutes) activateTranslations(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Input    models.TranslationActivationInput `json:"input"`
		Expected string                            `json:"expected_plan_sha256"`
	}
	if err := readIngestJSON(w, r, archive.MaxTranslationActivationBytes, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.TranslationActivation
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.TranslationWork.Activate(ctx, input.Input, input.Expected, rs.translationService().Durable.Now())
		return err
	})
	writeTranslationWork(w, result, err)
}

func (rs *nativeArchiveRoutes) translationActivation(w http.ResponseWriter, r *http.Request) {
	var result *models.TranslationActivation
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.TranslationWork.Activation(ctx, chi.URLParam(r, "activation"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	writeTranslationWork(w, result, err)
}

func (rs *nativeArchiveRoutes) heldAutomationTranslations(w http.ResponseWriter, r *http.Request) {
	after, limit := int64(0), archive.MaxTranslationActivationTargets
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
		translationWorkError(w, models.ErrTranslationWorkInvalid)
		return
	}
	var result []models.AutomationTranslationCandidate
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.AutomationTranslationImport.HeldTargets(ctx, chi.URLParam(r, "snapshot"), r.URL.Query().Get("expected_manifest_sha256"), after, limit)
		return err
	})
	writeTranslationWork(w, result, err)
}
