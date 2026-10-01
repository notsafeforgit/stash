package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

type backfillStatusInput struct {
	models.BackfillSubject
	Component string `json:"component"`
}

func (rs *ingestRoutes) backfillStatus(w http.ResponseWriter, r *http.Request) {
	var input backfillStatusInput
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := rs.service.BackfillStatus(r.Context(), ingestToken(r), input.BackfillSubject, input.Component)
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *ingestRoutes) completeBackfill(w http.ResponseWriter, r *http.Request) {
	var input models.BackfillCompletion
	if err := readIngestJSON(w, r, 1<<20, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := rs.service.CompleteBackfill(r.Context(), ingestToken(r), input)
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result.BackfillDecisionSummary)
}

// Historical assertions require application authentication. A producer can
// submit verified native coverage, but cannot invent a user acceptance/skip.
func (rs *nativeArchiveRoutes) importBackfills(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Records []models.LegacyBackfillRecord `json:"records"`
	}
	if err := readIngestJSON(w, r, 4<<20, &input); err != nil {
		ingestError(w, err)
		return
	}
	if len(input.Records) < 1 || len(input.Records) > 50 {
		ingestError(w, models.ErrBackfillInvalid)
		return
	}
	result := make([]models.BackfillDecisionSummary, 0, len(input.Records))
	now := time.Now()
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		for _, record := range input.Records {
			decision, err := rs.repo.SourceBackfill.ImportLegacy(ctx, record, now)
			if err != nil {
				return err
			}
			result = append(result, decision.BackfillDecisionSummary)
		}
		return nil
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) backfillStatus(w http.ResponseWriter, r *http.Request) {
	var input backfillStatusInput
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.BackfillStatus
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceBackfill.Status(ctx, input.BackfillSubject, input.Component)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) backfill(w http.ResponseWriter, r *http.Request) {
	var result *models.BackfillDecision
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceBackfill.Find(ctx, chi.URLParam(r, "decision"))
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
