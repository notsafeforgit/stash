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

func (rs *nativeArchiveRoutes) enrichmentRebindCandidates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	revision, err := strconv.Atoi(q.Get("collection_revision"))
	if err != nil || revision < 1 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var after *models.EnrichmentRebindCursor
	if q.Has("after_collection_revision") || q.Has("after_target") {
		previous, err := strconv.Atoi(q.Get("after_collection_revision"))
		if err != nil {
			ingestError(w, ingest.ErrInvalid)
			return
		}
		after = &models.EnrichmentRebindCursor{CollectionRevision: previous, TargetUUID: q.Get("after_target")}
	}
	var result []models.EnrichmentRebindCandidate
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.EnrichmentWork.RebindCandidates(ctx, chi.URLParam(r, "collection"), revision, after, limit)
		return err
	})
	writeEnrichmentWork(w, result, err)
}

func (rs *nativeArchiveRoutes) previewEnrichmentRebind(w http.ResponseWriter, r *http.Request) {
	var input models.EnrichmentRebindInput
	if err := readIngestJSON(w, r, 65536, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.EnrichmentRebindPlan
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.EnrichmentWork.PreviewRebind(ctx, input)
		return err
	})
	writeEnrichmentWork(w, result, err)
}

func (rs *nativeArchiveRoutes) rebindEnrichment(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Input              models.EnrichmentRebindInput `json:"input"`
		ExpectedPlanSHA256 string                       `json:"expected_plan_sha256"`
	}
	if err := readIngestJSON(w, r, 65536, &request); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.EnrichmentRebinding
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.EnrichmentWork.Rebind(ctx, request.Input, request.ExpectedPlanSHA256, time.Now())
		return err
	})
	writeEnrichmentWork(w, result, err)
}

func (rs *nativeArchiveRoutes) enrichmentRebinding(w http.ResponseWriter, r *http.Request) {
	var result *models.EnrichmentRebinding
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.EnrichmentWork.Rebinding(ctx, chi.URLParam(r, "rebinding"))
		return err
	})
	if err == nil && result == nil {
		http.NotFound(w, r)
		return
	}
	writeEnrichmentWork(w, result, err)
}
