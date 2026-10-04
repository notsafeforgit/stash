package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) publishDiscoveryMatch(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Expected      int    `json:"expected_target_revision"`
		DetailJobUUID string `json:"detail_job_uuid,omitempty"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := ingest.New(rs.repo).PublishDiscoveryMatch(r.Context(), models.DiscoveryPublicationInput{TargetUUID: chi.URLParam(r, "target"), ExpectedTargetRevision: input.Expected, DetailJobUUID: input.DetailJobUUID})
	writeDiscoveryWorker(w, result, err)
}

func (rs *nativeArchiveRoutes) discoveryPublication(w http.ResponseWriter, r *http.Request) {
	var result *models.DiscoveryMatchPublication
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryMatch.Publication(ctx, chi.URLParam(r, "target"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	writeDiscoveryWorker(w, result, err)
}

func (rs *nativeArchiveRoutes) discoveryPublishedRecords(w http.ResponseWriter, r *http.Request) {
	after, limit := -1, 100
	var err error
	if value := r.URL.Query().Get("after"); value != "" {
		after, err = strconv.Atoi(value)
	}
	if err == nil {
		if value := r.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
		}
	}
	if err != nil || limit < 1 || limit > 100 {
		writeDiscoveryWorker(w, nil, models.ErrDiscoveryInvalid)
		return
	}
	var result []models.DiscoveryPublishedRecord
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryMatch.PublishedRecords(ctx, chi.URLParam(r, "target"), after, limit)
		return err
	})
	writeDiscoveryWorker(w, result, err)
}
