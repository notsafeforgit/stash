package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) performerSourceAccounts(w http.ResponseWriter, r *http.Request) {
	after, limit, err := accountReviewPage(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result *models.PerformerSourceAccounts
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAccount.PerformerAccounts(ctx, chi.URLParam(r, "entity"), after, limit)
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

func (rs *nativeArchiveRoutes) performerSourceIdentities(w http.ResponseWriter, r *http.Request) {
	after, limit, err := accountReviewPage(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result *models.PerformerSourceIdentities
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAccount.PerformerIdentities(ctx, chi.URLParam(r, "entity"), after, limit)
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
