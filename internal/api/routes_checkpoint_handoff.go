package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) previewCheckpointHandoff(w http.ResponseWriter, r *http.Request) {
	var input models.CheckpointHandoffInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.CheckpointHandoffPlan
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.AutomationCheckpointImport.PreviewHandoff(ctx, input)
		return err
	})
	writeEnrichmentWork(w, result, err)
}

func (rs *nativeArchiveRoutes) acceptCheckpointHandoff(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Input    models.CheckpointHandoffInput `json:"input"`
		Expected string                        `json:"expected_plan_sha256"`
	}
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.CheckpointHandoff
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.AutomationCheckpointImport.AcceptHandoff(ctx, input.Input, input.Expected, time.Now())
		return err
	})
	writeEnrichmentWork(w, result, err)
}

func (rs *nativeArchiveRoutes) checkpointHandoff(w http.ResponseWriter, r *http.Request) {
	var result *models.CheckpointHandoff
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.AutomationCheckpointImport.Handoff(ctx, chi.URLParam(r, "handoff"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	writeEnrichmentWork(w, result, err)
}

func (rs *nativeArchiveRoutes) checkpointHandoffSeed(w http.ResponseWriter, r *http.Request) {
	var result *models.CheckpointHandoffSeed
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.AutomationCheckpointImport.HandoffSeed(ctx, chi.URLParam(r, "handoff"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	writeEnrichmentWork(w, result, err)
}
