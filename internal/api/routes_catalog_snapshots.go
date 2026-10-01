package api

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func readCatalogSnapshotBody(w http.ResponseWriter, r *http.Request, contentType string, limit int) ([]byte, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != contentType || r.Header.Get("Content-Encoding") != "" {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(limit)))
	if err != nil {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	return body, nil
}

func (rs *nativeArchiveRoutes) beginCatalogSnapshot(w http.ResponseWriter, r *http.Request) {
	body, err := readCatalogSnapshotBody(w, r, "application/json", scrape.CatalogManifestLimit)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result *models.CatalogSnapshot
	err = rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogSnapshot.Begin(ctx, body, r.Header.Get("X-Stash-Manifest-SHA256"), time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) receiveCatalogSnapshotChunk(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(chi.URLParam(r, "chunk"))
	if err != nil || index < 0 {
		ingestError(w, models.ErrCatalogSnapshotInvalid)
		return
	}
	body, err := readCatalogSnapshotBody(w, r, "application/x-ndjson", scrape.CatalogChunkLimit)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result *models.CatalogSnapshot
	err = rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogSnapshot.Receive(ctx, chi.URLParam(r, "snapshot"), r.Header.Get("X-Stash-Manifest-SHA256"), index, body, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) catalogSnapshot(w http.ResponseWriter, r *http.Request) {
	var result *models.CatalogSnapshot
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.CatalogSnapshot.Find(ctx, chi.URLParam(r, "snapshot"))
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
