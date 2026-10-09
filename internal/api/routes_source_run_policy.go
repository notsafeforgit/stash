package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *ingestRoutes) upgradeSourceRunPolicy(w http.ResponseWriter, r *http.Request) {
	var input models.SourceRunPolicyUpgradeInput
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.SourceRunPolicyUpgrade
	err := rs.service.Repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.service.Repo.SourceRun.UpgradePolicy(ctx, input, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *ingestRoutes) sourceRunPolicyUpgrade(w http.ResponseWriter, r *http.Request) {
	var result *models.SourceRunPolicyUpgrade
	err := rs.service.Repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.service.Repo.SourceRun.PolicyUpgrade(ctx, chi.URLParam(r, "upgrade"))
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	if result == nil {
		ingestError(w, ingest.ErrNotFound)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
