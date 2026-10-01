package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) roots(w http.ResponseWriter, r *http.Request) {
	var result []*models.MediaRoot
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MediaRoot.List(ctx, r.URL.Query().Get("after"), 50)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) putRoot(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedRevision int     `json:"expected_revision"`
		Label            string  `json:"label"`
		State            string  `json:"state"`
		ServerPath       *string `json:"server_path"`
		Reason           string  `json:"reason"`
	}
	if err := readIngestJSON(w, r, 12288, &input); err != nil {
		ingestError(w, err)
		return
	}
	definition := models.MediaRootDefinition{Label: input.Label, State: input.State}
	if input.ServerPath != nil {
		binding, err := archive.ProbeMediaRoot(*input.ServerPath)
		if err != nil {
			ingestError(w, ingest.ErrInvalid)
			return
		}
		definition.Binding = binding
	}
	var result *models.MediaRoot
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MediaRoot.Put(ctx, models.MediaRootInput{UUID: chi.URLParam(r, "root"), ExpectedRevision: input.ExpectedRevision, MediaRootDefinition: definition, Origin: "review", Reason: input.Reason})
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) collections(w http.ResponseWriter, r *http.Request) {
	var result []*models.SourceCollection
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceCollection.List(ctx, r.URL.Query().Get("after"), 50)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) putCollection(w http.ResponseWriter, r *http.Request) {
	var input models.SourceCollectionInput
	if err := readIngestJSON(w, r, 24576, &input); err != nil {
		ingestError(w, err)
		return
	}
	input.UUID, input.Origin = chi.URLParam(r, "collection"), "review"
	var result *models.SourceCollection
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceCollection.Put(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
