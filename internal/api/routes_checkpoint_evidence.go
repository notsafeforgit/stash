package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) previewCheckpointEvidence(w http.ResponseWriter, r *http.Request) {
	var input models.CheckpointEvidenceInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.CheckpointEvidencePlan
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.AutomationCheckpointImport.PreviewEvidence(ctx, input)
		return err
	})
	writeEnrichmentWork(w, result, err)
}

func (rs *nativeArchiveRoutes) acceptCheckpointEvidence(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Input    models.CheckpointEvidenceInput `json:"input"`
		Expected string                         `json:"expected_plan_sha256"`
	}
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.CheckpointEvidenceAcceptance
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.AutomationCheckpointImport.AcceptEvidence(ctx, input.Input, input.Expected, time.Now())
		return err
	})
	writeEnrichmentWork(w, result, err)
}

func (rs *nativeArchiveRoutes) checkpointEvidenceAcceptance(w http.ResponseWriter, r *http.Request) {
	var result *models.CheckpointEvidenceAcceptance
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.AutomationCheckpointImport.EvidenceAcceptance(ctx, chi.URLParam(r, "acceptance"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	writeEnrichmentWork(w, result, err)
}
