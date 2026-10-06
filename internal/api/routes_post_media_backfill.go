package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) postMediaBackfillPosts(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			ingestError(w, ingest.ErrInvalid)
			return
		}
	}
	var ret []string
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourcePostMedia.BackfillPosts(ctx, r.URL.Query().Get("after"), limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}

func (rs *nativeArchiveRoutes) postMediaBackfillPreview(w http.ResponseWriter, r *http.Request) {
	var ret *models.SourcePostMediaMatchPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourcePostMedia.PreviewBackfill(ctx, chi.URLParam(r, "post"))
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}

func (rs *nativeArchiveRoutes) applyPostMediaBackfill(w http.ResponseWriter, r *http.Request) {
	var input models.SourcePostMediaBackfillInput
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	if input.PostUUID != chi.URLParam(r, "post") {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var ret *models.SourcePostMediaBackfillResult
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourcePostMedia.Backfill(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}

func (rs *nativeArchiveRoutes) postMediaBackfillResult(w http.ResponseWriter, r *http.Request) {
	var ret *models.SourcePostMediaBackfillResult
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourcePostMedia.BackfillResult(ctx, chi.URLParam(r, "request"))
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	if ret == nil {
		http.NotFound(w, r)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}

func (rs *nativeArchiveRoutes) postMediaMatchedEvidence(w http.ResponseWriter, r *http.Request) {
	var ret []models.SourcePostMediaMatchedEvidence
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourcePostMedia.MatchedEvidence(ctx, chi.URLParam(r, "decision"))
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}
