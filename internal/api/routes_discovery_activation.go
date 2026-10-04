package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) previewDiscoveryActivation(w http.ResponseWriter, r *http.Request) {
	var input models.DiscoveryActivationInput
	if err := readIngestJSON(w, r, archive.MaxDiscoveryActivationBytes, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.DiscoveryActivationPlan
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryMatch.PreviewActivation(ctx, input)
		return err
	})
	writeDiscoveryWorker(w, result, err)
}

func (rs *nativeArchiveRoutes) activateDiscovery(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Input    models.DiscoveryActivationInput `json:"input"`
		Expected string                          `json:"expected_plan_sha256"`
	}
	if err := readIngestJSON(w, r, archive.MaxDiscoveryActivationBytes, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.DiscoveryActivation
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryMatch.Activate(ctx, input.Input, input.Expected, time.Now())
		return err
	})
	writeDiscoveryWorker(w, result, err)
}

func (rs *nativeArchiveRoutes) discoveryActivation(w http.ResponseWriter, r *http.Request) {
	var result *models.DiscoveryActivation
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryMatch.Activation(ctx, chi.URLParam(r, "activation"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	writeDiscoveryWorker(w, result, err)
}

func (rs *nativeArchiveRoutes) discoveryMatchTarget(w http.ResponseWriter, r *http.Request) {
	var result *models.DiscoveryMatchTarget
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryMatch.Target(ctx, chi.URLParam(r, "target"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	writeDiscoveryWorker(w, result, err)
}

func (rs *nativeArchiveRoutes) discoveryMatchReview(w http.ResponseWriter, r *http.Request) {
	var result *models.DiscoveryMatchReview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryMatch.Review(ctx, chi.URLParam(r, "target"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	writeDiscoveryWorker(w, result, err)
}

func discoveryReviewPagination(r *http.Request) (int64, int, error) {
	after, limit := int64(0), 100
	var err error
	if value := r.URL.Query().Get("after"); value != "" {
		after, err = strconv.ParseInt(value, 10, 64)
	}
	if err == nil {
		if value := r.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
		}
	}
	if err != nil || after < 0 || limit < 1 || limit > 100 {
		return 0, 0, models.ErrDiscoveryInvalid
	}
	return after, limit, nil
}

func (rs *nativeArchiveRoutes) discoveryMatchCandidates(w http.ResponseWriter, r *http.Request) {
	after, limit, err := discoveryReviewPagination(r)
	if err != nil {
		writeDiscoveryWorker(w, nil, err)
		return
	}
	var result []models.DiscoveryMatchCandidate
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryMatch.Candidates(ctx, chi.URLParam(r, "target"), after, limit)
		return err
	})
	writeDiscoveryWorker(w, result, err)
}

func (rs *nativeArchiveRoutes) discoveryMatchEvidence(w http.ResponseWriter, r *http.Request) {
	after, limit, err := discoveryReviewPagination(r)
	id, parseErr := strconv.ParseInt(chi.URLParam(r, "candidate"), 10, 64)
	if err != nil || parseErr != nil || id < 1 || after > archive.MaxDiscoveryPages {
		writeDiscoveryWorker(w, nil, models.ErrDiscoveryInvalid)
		return
	}
	var result []models.DiscoveryMatchEvidence
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryMatch.Evidence(ctx, id, int(after), limit)
		return err
	})
	writeDiscoveryWorker(w, result, err)
}

func (rs *nativeArchiveRoutes) discoveryListingReview(w http.ResponseWriter, r *http.Request) {
	var result *models.DiscoveryListing
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryJob.Listing(ctx, chi.URLParam(r, "listing"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	writeDiscoveryWorker(w, result, err)
}

func (rs *nativeArchiveRoutes) discoveryListingPages(w http.ResponseWriter, r *http.Request) {
	after, limit, err := discoveryReviewPagination(r)
	if err != nil || after > archive.MaxDiscoveryPages {
		writeDiscoveryWorker(w, nil, models.ErrDiscoveryInvalid)
		return
	}
	var result []models.DiscoveryPageReceipt
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.DiscoveryJob.Pages(ctx, chi.URLParam(r, "listing"), int(after), limit)
		return err
	})
	writeDiscoveryWorker(w, result, err)
}
