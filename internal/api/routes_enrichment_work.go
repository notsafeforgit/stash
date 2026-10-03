package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func writeEnrichmentWork(w http.ResponseWriter, value any, err error) {
	switch {
	case err == nil:
		ingestJSON(w, http.StatusOK, value)
	case errors.Is(err, models.ErrEnrichmentInvalid):
		ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_enrichment_work"})
	case errors.Is(err, models.ErrEnrichmentConflict), errors.Is(err, models.ErrSourcePostConflict), errors.Is(err, models.ErrSourcePostForgotten):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "enrichment_work_changed"})
	default:
		ingestError(w, err)
	}
}

func (rs *nativeArchiveRoutes) readEnrichment(w http.ResponseWriter, r *http.Request, read func(context.Context) (any, error)) {
	var value any
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		value, err = read(ctx)
		return err
	})
	writeEnrichmentWork(w, value, err)
}

func (rs *nativeArchiveRoutes) enrichmentTarget(w http.ResponseWriter, r *http.Request) {
	rs.readEnrichment(w, r, func(ctx context.Context) (any, error) {
		value, err := rs.repo.EnrichmentWork.Target(ctx, chi.URLParam(r, "target"))
		if err == nil && value == nil {
			err = ingest.ErrNotFound
		}
		return value, err
	})
}

func (rs *nativeArchiveRoutes) enrichmentCompletion(w http.ResponseWriter, r *http.Request) {
	rs.readEnrichment(w, r, func(ctx context.Context) (any, error) {
		value, err := rs.repo.EnrichmentWork.Completion(ctx, chi.URLParam(r, "completion"))
		if err == nil && value == nil {
			err = ingest.ErrNotFound
		}
		return value, err
	})
}

func (rs *nativeArchiveRoutes) enrichmentTargets(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	q := models.EnrichmentTargetQuery{PostUUID: chi.URLParam(r, "post"), CollectionUUID: chi.URLParam(r, "collection"),
		State: r.URL.Query().Get("state"), After: r.URL.Query().Get("after"), Limit: limit}
	rs.readEnrichment(w, r, func(ctx context.Context) (any, error) {
		if q.PostUUID != "" {
			if !ingest.ValidUUID(q.PostUUID) {
				return nil, ingest.ErrInvalid
			}
			post, err := rs.repo.SourceEvidence.FindPost(ctx, q.PostUUID)
			if err != nil {
				return nil, err
			}
			if post == nil {
				return nil, ingest.ErrNotFound
			}
		} else {
			if !ingest.ValidUUID(q.CollectionUUID) {
				return nil, ingest.ErrInvalid
			}
			collection, err := rs.repo.SourceCollection.Find(ctx, q.CollectionUUID)
			if err != nil {
				return nil, err
			}
			if collection == nil {
				return nil, ingest.ErrNotFound
			}
		}
		return rs.repo.EnrichmentWork.Targets(ctx, q)
	})
}

func (rs *nativeArchiveRoutes) enrichmentTargetHistory(w http.ResponseWriter, r *http.Request) {
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
	rs.readEnrichment(w, r, func(ctx context.Context) (any, error) {
		target, err := rs.repo.EnrichmentWork.Target(ctx, chi.URLParam(r, "target"))
		if err != nil {
			return nil, err
		}
		if target == nil {
			return nil, ingest.ErrNotFound
		}
		return rs.repo.EnrichmentWork.History(ctx, target.UUID, after, limit)
	})
}

func (rs *nativeArchiveRoutes) createEnrichmentTarget(w http.ResponseWriter, r *http.Request) {
	var input struct {
		URLUUID            string                    `json:"url_uuid"`
		CollectionUUID     string                    `json:"collection_uuid"`
		CollectionRevision int                       `json:"collection_revision"`
		Policy             string                    `json:"policy"`
		Schedule           models.EnrichmentSchedule `json:"schedule"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	var ret *models.EnrichmentTarget
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		postID := chi.URLParam(r, "post")
		if !ingest.ValidUUID(postID) {
			return ingest.ErrInvalid
		}
		post, err := rs.repo.SourceEvidence.FindPost(ctx, postID)
		if err != nil {
			return err
		}
		if post == nil {
			return ingest.ErrNotFound
		}
		ret, err = rs.repo.EnrichmentWork.RetainTarget(ctx, models.EnrichmentTargetInput{PostUUID: postID, URLUUID: input.URLUUID,
			CollectionUUID: input.CollectionUUID, CollectionRevision: input.CollectionRevision, Policy: input.Policy, Origin: "review"}, input.Schedule, time.Now())
		return err
	})
	writeEnrichmentWork(w, ret, err)
}

func (rs *nativeArchiveRoutes) scheduleEnrichmentTarget(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision int                       `json:"expected_revision"`
		Schedule models.EnrichmentSchedule `json:"schedule"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	var ret *models.EnrichmentTarget
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.EnrichmentWork.Schedule(ctx, chi.URLParam(r, "target"), input.Revision, input.Schedule, time.Now())
		return err
	})
	writeEnrichmentWork(w, ret, err)
}
