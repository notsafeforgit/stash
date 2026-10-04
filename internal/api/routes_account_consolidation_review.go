package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) previewAccountConsolidation(w http.ResponseWriter, r *http.Request) {
	var input models.AccountConsolidationReviewInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.AccountConsolidationReviewPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAccount.PreviewConsolidationReview(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyAccountConsolidation(w http.ResponseWriter, r *http.Request) {
	var input models.AccountConsolidationReviewApplyInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.AccountConsolidationReview
	var replayed bool
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, replayed, err = rs.repo.SourceAccount.ApplyConsolidationReview(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"review": result, "replayed": replayed})
}

// POST carries the exact saved request without exposing it in URLs or logs.
// This endpoint only reads the immutable event and never applies a change.
func (rs *nativeArchiveRoutes) checkAccountConsolidation(w http.ResponseWriter, r *http.Request) {
	var input models.AccountConsolidationReviewApplyInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	if input.RequestUUID != chi.URLParam(r, "request") {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.AccountConsolidationReview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAccount.ConsolidationReview(ctx, input)
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

func (rs *nativeArchiveRoutes) accountConsolidationHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "account")
	cursor, limit, err := accountReviewPage(r)
	after := 0
	if cursor != "" && err == nil {
		after, err = strconv.Atoi(cursor)
	}
	if err != nil || after < 0 || !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	result := []models.AccountConsolidationRecord{}
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		account, err := rs.repo.SourceAccount.Find(ctx, id)
		if err != nil {
			return err
		}
		if account == nil {
			return ingest.ErrNotFound
		}
		items, err := rs.repo.SourceAccount.ConsolidationHistory(ctx, id, after, limit)
		for _, item := range items {
			result = append(result, item.ReviewRecord())
		}
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
