package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func (rs *nativeArchiveRoutes) beginAutomationSnapshot(w http.ResponseWriter, r *http.Request) {
	body, err := readCatalogSnapshotBody(w, r, "application/json", scrape.CatalogManifestLimit)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result *models.AutomationSnapshot
	err = rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.AutomationSnapshot.Begin(ctx, body, r.Header.Get("X-Stash-Manifest-SHA256"), time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) receiveAutomationSnapshotChunk(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(chi.URLParam(r, "chunk"))
	if err != nil || index < 0 {
		ingestError(w, models.ErrAutomationSnapshotInvalid)
		return
	}
	body, err := readCatalogSnapshotBody(w, r, "application/x-ndjson", scrape.CatalogChunkLimit)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result *models.AutomationSnapshot
	err = rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.AutomationSnapshot.Receive(ctx, chi.URLParam(r, "snapshot"), r.Header.Get("X-Stash-Manifest-SHA256"), index, body, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) automationSnapshot(w http.ResponseWriter, r *http.Request) {
	var result *models.AutomationSnapshot
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.AutomationSnapshot.Find(ctx, chi.URLParam(r, "snapshot"))
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
