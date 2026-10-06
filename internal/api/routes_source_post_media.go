package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) postMediaAssociation(w http.ResponseWriter, r *http.Request) {
	var ret *models.SourcePostMediaAssociation
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourcePostMedia.Association(ctx, chi.URLParam(r, "post"), chi.URLParam(r, "entity"))
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}

func (rs *nativeArchiveRoutes) decidePostMedia(w http.ResponseWriter, r *http.Request) {
	var request models.SourcePostMediaInput
	if err := readIngestJSON(w, r, 65536, &request); err != nil {
		ingestError(w, err)
		return
	}
	if request.PostUUID != chi.URLParam(r, "post") || request.MediaUUID != chi.URLParam(r, "entity") || request.Origin != "review" {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var ret *models.SourcePostMediaDecision
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourcePostMedia.Decide(ctx, request)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}

func (rs *nativeArchiveRoutes) postMediaDecision(w http.ResponseWriter, r *http.Request) {
	var ret *models.SourcePostMediaDecision
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourcePostMedia.Decision(ctx, chi.URLParam(r, "decision"))
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

func (rs *nativeArchiveRoutes) postMediaHistory(w http.ResponseWriter, r *http.Request) {
	after, limit := 0, 25
	var err error
	if value := r.URL.Query().Get("after"); value != "" {
		after, err = strconv.Atoi(value)
	}
	if err != nil || after < 0 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
	}
	if err != nil || limit < 1 || limit > 100 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var ret []models.SourcePostMediaDecision
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.SourcePostMedia.History(ctx, chi.URLParam(r, "post"), chi.URLParam(r, "entity"), after, limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, ret)
}
