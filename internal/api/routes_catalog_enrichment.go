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

func (rs *nativeArchiveRoutes) advanceCatalogEnrichmentImport(w http.ResponseWriter, r *http.Request) {
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
	var result *models.CatalogEnrichmentImport
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogEnrichmentImport.Advance(ctx, chi.URLParam(r, "snapshot"), input.ManifestSHA256, *input.After, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) catalogEnrichmentImport(w http.ResponseWriter, r *http.Request) {
	var result *models.CatalogEnrichmentImport
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogEnrichmentImport.Find(ctx, chi.URLParam(r, "snapshot"))
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

func (rs *nativeArchiveRoutes) catalogEnrichmentRecords(w http.ResponseWriter, r *http.Request) {
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
	var result []models.CatalogEnrichmentRecord
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		id := chi.URLParam(r, "snapshot")
		parent, err := rs.repo.CatalogEnrichmentImport.Find(ctx, id)
		if err != nil {
			return err
		}
		if parent == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.CatalogEnrichmentImport.Records(ctx, id, after, limit)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) catalogEnrichmentRecord(w http.ResponseWriter, r *http.Request) {
	ordinal, err := strconv.ParseInt(chi.URLParam(r, "ordinal"), 10, 64)
	if err != nil {
		ingestError(w, models.ErrCatalogSnapshotInvalid)
		return
	}
	var result *models.CatalogEnrichmentRecordDetails
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogEnrichmentImport.Record(ctx, chi.URLParam(r, "snapshot"), ordinal)
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

func (rs *nativeArchiveRoutes) enrichmentReceipt(w http.ResponseWriter, r *http.Request) {
	var result *models.SourceEnrichmentReceipt
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceEnrichmentReceipt.Find(ctx, chi.URLParam(r, "receipt"))
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

func (rs *nativeArchiveRoutes) postEnrichmentReceipts(w http.ResponseWriter, r *http.Request) {
	limit, err := documentLimit(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	post := chi.URLParam(r, "post")
	var result []models.SourceEnrichmentReceipt
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		if !ingest.ValidUUID(post) {
			return models.ErrCatalogSnapshotInvalid
		}
		parent, err := rs.repo.SourceEvidence.FindPost(ctx, post)
		if err != nil {
			return err
		}
		if parent == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceEnrichmentReceipt.PostReceipts(ctx, post, r.URL.Query().Get("after"), limit)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
