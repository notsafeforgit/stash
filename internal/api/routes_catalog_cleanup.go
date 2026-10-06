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

func (rs *nativeArchiveRoutes) advanceCatalogCleanupImport(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ManifestSHA256 string `json:"expected_manifest_sha256"`
		After          *int64 `json:"after"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	if input.After == nil {
		ingestError(w, models.ErrCatalogSnapshotInvalid)
		return
	}
	var result *models.CatalogCleanupImport
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogCleanupImport.Advance(ctx, chi.URLParam(r, "snapshot"), input.ManifestSHA256, *input.After, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) catalogCleanupImport(w http.ResponseWriter, r *http.Request) {
	var result *models.CatalogCleanupImport
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogCleanupImport.Find(ctx, chi.URLParam(r, "snapshot"))
		return err
	})
	if err == nil && result == nil {
		err = ingest.ErrNotFound
	}
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) catalogCleanupRecords(w http.ResponseWriter, r *http.Request) {
	after, limit := int64(0), 100
	var err error
	if value := r.URL.Query().Get("after"); value != "" {
		after, err = strconv.ParseInt(value, 10, 64)
	}
	if err == nil {
		if value := r.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
		}
	}
	if err != nil {
		ingestError(w, models.ErrCatalogSnapshotInvalid)
		return
	}
	var result []models.CatalogCleanupRecord
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		id := chi.URLParam(r, "snapshot")
		parent, err := rs.repo.CatalogCleanupImport.Find(ctx, id)
		if err != nil {
			return err
		}
		if parent == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.CatalogCleanupImport.Records(ctx, id, after, limit)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) catalogCleanupRecord(w http.ResponseWriter, r *http.Request) {
	ordinal, err := strconv.ParseInt(chi.URLParam(r, "ordinal"), 10, 64)
	if err != nil {
		ingestError(w, models.ErrCatalogSnapshotInvalid)
		return
	}
	var result *models.CatalogCleanupRecordDetails
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogCleanupImport.Record(ctx, chi.URLParam(r, "snapshot"), ordinal)
		return err
	})
	if err == nil && result == nil {
		err = ingest.ErrNotFound
	}
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) cleanupIntent(w http.ResponseWriter, r *http.Request) {
	var result *models.SourceCleanupIntent
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceCleanupIntent.Find(ctx, chi.URLParam(r, "intent"))
		return err
	})
	if err == nil && result == nil {
		err = ingest.ErrNotFound
	}
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) collectionCleanupIntents(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	collection := chi.URLParam(r, "collection")
	var result []models.SourceCleanupIntent
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		if !ingest.ValidUUID(collection) {
			return models.ErrCatalogSnapshotInvalid
		}
		parent, err := rs.repo.SourceCollection.Find(ctx, collection)
		if err != nil {
			return err
		}
		if parent == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceCleanupIntent.CollectionIntents(ctx, collection, r.URL.Query().Get("after"), limit)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
