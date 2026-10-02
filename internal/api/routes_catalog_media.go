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

func (rs *nativeArchiveRoutes) beginCatalogMediaImport(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ManifestSHA256     string `json:"expected_manifest_sha256"`
		RootUUID           string `json:"root_uuid"`
		RootRevision       int    `json:"root_revision"`
		CollectionRevision int    `json:"collection_revision"`
		LibraryRootPath    string `json:"library_root_path"`
	}
	if err := readIngestJSON(w, r, 8192, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.CatalogMediaImport
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogMediaImport.Begin(ctx, models.CatalogMediaBinding{
			SnapshotUUID: chi.URLParam(r, "snapshot"), ManifestSHA256: input.ManifestSHA256,
			RootUUID: input.RootUUID, RootRevision: input.RootRevision, CollectionRevision: input.CollectionRevision, LibraryRootPath: input.LibraryRootPath,
		}, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) advanceCatalogMediaImport(w http.ResponseWriter, r *http.Request) {
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
	var result *models.CatalogMediaImport
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogMediaImport.Advance(ctx, chi.URLParam(r, "snapshot"), input.ManifestSHA256, *input.After, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) catalogMediaImport(w http.ResponseWriter, r *http.Request) {
	var result *models.CatalogMediaImport
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogMediaImport.Find(ctx, chi.URLParam(r, "snapshot"))
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

func (rs *nativeArchiveRoutes) catalogMediaRecords(w http.ResponseWriter, r *http.Request) {
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
	var result []models.CatalogMediaRecord
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		id := chi.URLParam(r, "snapshot")
		parent, err := rs.repo.CatalogMediaImport.Find(ctx, id)
		if err != nil {
			return err
		}
		if parent == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.CatalogMediaImport.Records(ctx, id, after, limit)
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) catalogMediaRecord(w http.ResponseWriter, r *http.Request) {
	ordinal, err := strconv.ParseInt(chi.URLParam(r, "ordinal"), 10, 64)
	if err != nil {
		ingestError(w, models.ErrCatalogSnapshotInvalid)
		return
	}
	var result *models.CatalogMediaRecordDetails
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogMediaImport.Record(ctx, chi.URLParam(r, "snapshot"), ordinal)
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
