package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/pkg/models"
)

func archiveReviewError(w http.ResponseWriter, err error) {
	if errors.Is(err, models.ErrArchiveReviewInvalid) {
		ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_review_queue_request"})
		return
	}
	nativeArchiveError(w, err)
}

func (rs *nativeArchiveRoutes) archiveReviewQueue(w http.ResponseWriter, r *http.Request) {
	filter := models.ArchiveReviewFilter{Kind: chi.URLParam(r, "kind"), After: r.URL.Query().Get("after"), Limit: 25}
	for key, values := range r.URL.Query() {
		if (key != "after" && key != "limit") || len(values) != 1 || values[0] == "" {
			archiveReviewError(w, models.ErrArchiveReviewInvalid)
			return
		}
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		filter.Limit, err = strconv.Atoi(value)
		if err != nil {
			archiveReviewError(w, models.ErrArchiveReviewInvalid)
			return
		}
	}
	var page *models.ArchiveReviewPage
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		page, err = rs.repo.ArchiveReview.Queue(ctx, filter)
		return err
	})
	if err != nil {
		archiveReviewError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, page)
}
