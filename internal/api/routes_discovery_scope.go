package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) discoveryScopeCandidates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	revision, err := strconv.Atoi(q.Get("collection_revision"))
	if err != nil || revision < 1 {
		writeDiscoveryWorker(w, nil, models.ErrDiscoveryInvalid)
		return
	}
	limit := 100
	if q.Has("limit") {
		limit, err = strconv.Atoi(q.Get("limit"))
	}
	if err != nil || revision < 1 {
		writeDiscoveryWorker(w, nil, models.ErrDiscoveryInvalid)
		return
	}
	var result []models.DiscoveryScopeCandidate
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryJob.ScopeCandidates(ctx, chi.URLParam(r, "collection"), revision, q.Get("after"), limit)
		return err
	})
	writeDiscoveryWorker(w, result, err)
}

func (rs *nativeArchiveRoutes) previewDiscoveryScope(w http.ResponseWriter, r *http.Request) {
	var input models.DiscoveryScopeInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.DiscoveryScopePlan
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryJob.PreviewScope(ctx, input)
		return err
	})
	writeDiscoveryWorker(w, result, err)
}

func (rs *nativeArchiveRoutes) reviewDiscoveryScope(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Input    models.DiscoveryScopeInput `json:"input"`
		Expected string                     `json:"expected_plan_sha256"`
	}
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.DiscoveryScopeReview
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryJob.ReviewScope(ctx, input.Input, input.Expected, time.Now())
		return err
	})
	writeDiscoveryWorker(w, result, err)
}

func (rs *nativeArchiveRoutes) discoveryScopeReview(w http.ResponseWriter, r *http.Request) {
	var result *models.DiscoveryScopeReview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryJob.ScopeReview(ctx, chi.URLParam(r, "review"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	writeDiscoveryWorker(w, result, err)
}
