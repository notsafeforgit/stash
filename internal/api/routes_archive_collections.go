package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) roots(w http.ResponseWriter, r *http.Request) {
	filter, err := definitionFilter(r)
	if err != nil || filter.Kind != "" {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []*models.MediaRoot
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MediaRoot.Search(ctx, filter)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func definitionFilter(r *http.Request) (models.SourceDefinitionFilter, error) {
	after, limit, err := accountReviewPage(r)
	if r.URL.Query().Get("limit") == "" {
		limit = 50
	}
	return models.SourceDefinitionFilter{After: after, Limit: limit, Query: r.URL.Query().Get("q"), State: r.URL.Query().Get("state"), Kind: r.URL.Query().Get("kind")}, err
}

func (rs *nativeArchiveRoutes) root(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "root")
	if !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.MediaRoot
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MediaRoot.Find(ctx, id)
		if err == nil && result == nil {
			return ingest.ErrNotFound
		}
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
	filter, err := definitionFilter(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result []*models.SourceCollection
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceCollection.Search(ctx, filter)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) collectionHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "collection")
	after, limit, err := accountReviewPage(r)
	revision := 0
	if after != "" && err == nil {
		revision, err = strconv.Atoi(after)
	}
	if err != nil || revision < 0 || !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.SourceCollectionRevision
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		collection, err := rs.repo.SourceCollection.Find(ctx, id)
		if err != nil {
			return err
		}
		if collection == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceCollection.History(ctx, id, revision, limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) collection(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "collection")
	if !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.SourceCollection
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceCollection.Find(ctx, id)
		if err == nil && result == nil {
			return ingest.ErrNotFound
		}
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

func (rs *nativeArchiveRoutes) collectionPostMemberships(w http.ResponseWriter, r *http.Request) {
	rs.membershipPage(w, r, false)
}

func (rs *nativeArchiveRoutes) postCollectionMemberships(w http.ResponseWriter, r *http.Request) {
	rs.membershipPage(w, r, true)
}

func (rs *nativeArchiveRoutes) membershipPage(w http.ResponseWriter, r *http.Request, byPost bool) {
	id, after, limit := chi.URLParam(r, "collection"), r.URL.Query().Get("after"), 50
	if byPost {
		id = chi.URLParam(r, "post")
	}
	var err error
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
	}
	if err != nil || limit < 1 || limit > 100 || !ingest.ValidUUID(id) || (after != "" && !ingest.ValidUUID(after)) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.CollectionPostMembership
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		if byPost {
			post, err := rs.repo.SourceEvidence.FindPost(ctx, id)
			if err != nil {
				return err
			}
			if post == nil {
				return ingest.ErrNotFound
			}
			result, err = rs.repo.SourceCollection.PostMemberships(ctx, id, after, limit)
			return err
		}
		collection, err := rs.repo.SourceCollection.Find(ctx, id)
		if err != nil {
			return err
		}
		if collection == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceCollection.Memberships(ctx, id, after, limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
