package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) sourceFileHistory(w http.ResponseWriter, r *http.Request) {
	var result *models.SourceFileHistory
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceFileHistory.Find(ctx, chi.URLParam(r, "history"))
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

func (rs *nativeArchiveRoutes) fileHistoryPage(w http.ResponseWriter, r *http.Request, claim bool) {
	after, limit := r.URL.Query().Get("after"), 100
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil {
			ingestError(w, models.ErrSourceFileHistoryInvalid)
			return
		}
	}
	var result []models.SourceFileHistory
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		if claim {
			id := chi.URLParam(r, "claim")
			parent, err := rs.repo.SourceFile.ContentClaim(ctx, id)
			if err != nil {
				return err
			}
			if parent == nil {
				return ingest.ErrNotFound
			}
			result, err = rs.repo.SourceFileHistory.ClaimHistory(ctx, id, after, limit)
			return err
		}
		id := chi.URLParam(r, "observation")
		parent, err := rs.repo.SourceFile.Observation(ctx, id)
		if err != nil {
			return err
		}
		if parent == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceFileHistory.ObservationHistory(ctx, id, after, limit)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) observationFileHistory(w http.ResponseWriter, r *http.Request) {
	rs.fileHistoryPage(w, r, false)
}

func (rs *nativeArchiveRoutes) claimFileHistory(w http.ResponseWriter, r *http.Request) {
	rs.fileHistoryPage(w, r, true)
}
