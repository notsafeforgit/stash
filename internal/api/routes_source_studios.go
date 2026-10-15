package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) collectionStudio(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "collection")
	if !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.SourceStudioAssignment
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceStudio.Assignment(ctx, id)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) putCollectionStudio(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "collection")
	var input models.SourceStudioInput
	if !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.SourceStudioAssignment
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceStudio.Assign(ctx, id, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) postStudios(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "post")
	if !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.SourceStudio
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		post, err := rs.repo.SourceEvidence.FindPost(ctx, id)
		if err != nil {
			return err
		}
		if post == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceStudio.ForPost(ctx, id)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
