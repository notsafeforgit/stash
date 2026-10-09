package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *ingestRoutes) upgradeMetadataWorkerPolicy(w http.ResponseWriter, r *http.Request) {
	var input models.WorkerPolicyUpgradeInput
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.WorkerPolicyUpgrade
	err := rs.service.Repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.service.Repo.WorkerPolicy.Upgrade(ctx, input, time.Now())
		return err
	})
	writeEnrichmentWork(w, result, err)
}

func (rs *ingestRoutes) metadataWorkerPolicyUpgrade(w http.ResponseWriter, r *http.Request) {
	var result *models.WorkerPolicyUpgrade
	err := rs.service.Repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.service.Repo.WorkerPolicy.Receipt(ctx, chi.URLParam(r, "upgrade"))
		return err
	})
	if err != nil {
		writeEnrichmentWork(w, nil, err)
		return
	}
	if result == nil {
		ingestError(w, ingest.ErrNotFound)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
