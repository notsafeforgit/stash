package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) readTranslation(w http.ResponseWriter, r *http.Request, read func(context.Context) (any, error)) {
	var result any
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = read(ctx)
		return err
	})
	if errors.Is(err, models.ErrSourceTranslationInvalid) {
		err = ingest.ErrInvalid
	}
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) translation(w http.ResponseWriter, r *http.Request) {
	rs.readTranslation(w, r, func(ctx context.Context) (any, error) {
		result, err := rs.repo.SourceTranslation.Find(ctx, chi.URLParam(r, "translation"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return result, err
	})
}

func (rs *nativeArchiveRoutes) translationEvidence(w http.ResponseWriter, r *http.Request) {
	rs.readTranslation(w, r, func(ctx context.Context) (any, error) {
		result, err := rs.repo.SourceTranslation.Evidence(ctx, chi.URLParam(r, "evidence"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return result, err
	})
}

func (rs *nativeArchiveRoutes) postTranslations(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	query := models.SourceTranslationQuery{PostUUID: chi.URLParam(r, "post"), After: r.URL.Query().Get("after"), Limit: limit, OriginalSHA256: r.URL.Query().Get("original_sha256")}
	if r.URL.Query().Has("target_language") {
		value := r.URL.Query().Get("target_language")
		query.TargetLanguage = &value
	}
	rs.readTranslation(w, r, func(ctx context.Context) (any, error) {
		if !ingest.ValidUUID(query.PostUUID) {
			return nil, ingest.ErrInvalid
		}
		post, err := rs.repo.SourceEvidence.FindPost(ctx, query.PostUUID)
		if err != nil {
			return nil, err
		}
		if post == nil {
			return nil, ingest.ErrNotFound
		}
		return rs.repo.SourceTranslation.PostEvidence(ctx, query)
	})
}
