package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) translationDefault(w http.ResponseWriter, r *http.Request) {
	var result *models.TranslationPolicyDefault
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.TranslationPolicy.Default(ctx, chi.URLParam(r, "namespace"))
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) putTranslationDefault(w http.ResponseWriter, r *http.Request) {
	var input models.TranslationPolicyDefault
	if err := readIngestJSON(w, r, 8192, &input); err != nil {
		ingestError(w, err)
		return
	}
	input.Namespace = chi.URLParam(r, "namespace")
	var result *models.TranslationPolicyDefault
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.TranslationPolicy.PutDefault(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) backfillTranslations(w http.ResponseWriter, r *http.Request) {
	var input models.TranslationBackfillInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result []models.TranslationBackfillResult
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceTranslation.Backfill(ctx, input, time.Now().UTC())
		return err
	})
	if err != nil {
		translationWorkError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) translationPolicy(w http.ResponseWriter, r *http.Request) {
	var result *models.TranslationPolicy
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.TranslationPolicy.Find(ctx, chi.URLParam(r, "collection"))
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) putTranslationPolicy(w http.ResponseWriter, r *http.Request) {
	var input models.TranslationPolicyInput
	if err := readIngestJSON(w, r, 8192, &input); err != nil {
		ingestError(w, err)
		return
	}
	input.CollectionUUID, input.Origin = chi.URLParam(r, "collection"), "review"
	var result *models.TranslationPolicy
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.TranslationPolicy.Put(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) translationPolicyHistory(w http.ResponseWriter, r *http.Request) {
	after := 0
	if value := r.URL.Query().Get("after"); value != "" {
		var err error
		after, err = strconv.Atoi(value)
		if err != nil || after < 0 {
			ingestError(w, ingest.ErrInvalid)
			return
		}
	}
	var result []*models.TranslationPolicy
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.TranslationPolicy.History(ctx, chi.URLParam(r, "collection"), after, 50)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) captureTranslationDecision(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.Atoi(r.URL.Query().Get("collection_revision"))
	if err != nil || revision <= 0 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	input := models.CollectionCapture{CaptureUUID: chi.URLParam(r, "capture"), CollectionUUID: r.URL.Query().Get("collection_uuid"), CollectionRevision: revision}
	var result *models.CaptureTranslationDecision
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.TranslationPolicy.CaptureDecision(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	if result == nil {
		ingestJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
