package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) translationRequest(w http.ResponseWriter, r *http.Request) {
	rs.readTranslation(w, r, func(ctx context.Context) (any, error) {
		result, err := rs.repo.TranslationWork.Request(ctx, chi.URLParam(r, "request"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return result, err
	})
}

func (rs *nativeArchiveRoutes) translationRequestCache(w http.ResponseWriter, r *http.Request) {
	rs.readTranslation(w, r, func(ctx context.Context) (any, error) {
		request, err := rs.repo.TranslationWork.Request(ctx, chi.URLParam(r, "request"))
		if err != nil {
			return nil, err
		}
		if request == nil {
			return nil, ingest.ErrNotFound
		}
		// An existing request with no cache is valid pending work.
		return rs.repo.TranslationWork.Cache(ctx, request.UUID)
	})
}

func (rs *nativeArchiveRoutes) translationTarget(w http.ResponseWriter, r *http.Request) {
	rs.readTranslation(w, r, func(ctx context.Context) (any, error) {
		result, err := rs.repo.TranslationWork.Target(ctx, chi.URLParam(r, "target"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return result, err
	})
}

func (rs *nativeArchiveRoutes) translationTargets(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	query := models.TranslationTargetQuery{RequestUUID: chi.URLParam(r, "request"), PostUUID: chi.URLParam(r, "post"),
		State: r.URL.Query().Get("state"), After: r.URL.Query().Get("after"), Limit: limit}
	rs.readTranslation(w, r, func(ctx context.Context) (any, error) {
		if query.RequestUUID != "" {
			request, err := rs.repo.TranslationWork.Request(ctx, query.RequestUUID)
			if err != nil {
				return nil, err
			}
			if request == nil {
				return nil, ingest.ErrNotFound
			}
		} else {
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
		}
		return rs.repo.TranslationWork.Targets(ctx, query)
	})
}

func (rs *nativeArchiveRoutes) translationTargetHistory(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	after := 0
	if value := r.URL.Query().Get("after"); value != "" {
		after, err = strconv.Atoi(value)
		if err != nil || after < 0 {
			ingestError(w, ingest.ErrInvalid)
			return
		}
	}
	rs.readTranslation(w, r, func(ctx context.Context) (any, error) {
		target, err := rs.repo.TranslationWork.Target(ctx, chi.URLParam(r, "target"))
		if err != nil {
			return nil, err
		}
		if target == nil {
			return nil, ingest.ErrNotFound
		}
		return rs.repo.TranslationWork.TargetHistory(ctx, target.UUID, after, limit)
	})
}
