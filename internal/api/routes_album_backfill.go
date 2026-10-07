package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) albumPosts(w http.ResponseWriter, r *http.Request) {
	after, limit := r.URL.Query().Get("after"), 50
	if after != "" && !ingest.ValidUUID(after) {
		albumError(w, gallery.ErrAlbumWorkInvalid)
		return
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			albumError(w, gallery.ErrAlbumWorkInvalid)
			return
		}
	}
	var rows []models.SelectedSourcePost
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		rows, err = rs.repo.SourceAttachment.SelectedPosts(ctx, after, limit)
		return err
	})
	if err != nil {
		albumError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, rows)
}

func (rs *nativeArchiveRoutes) albumService() *gallery.AlbumBackfill {
	if rs.albums != nil {
		return rs.albums
	}
	return gallery.NewAlbumBackfill(rs.repo)
}

func albumError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gallery.ErrAlbumWorkInvalid), errors.Is(err, models.ErrSourceAlbumPolicy):
		ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_album_request"})
	case errors.Is(err, gallery.ErrAlbumWorkNotFound):
		ingestJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	case errors.Is(err, models.ErrSourceAlbumLimit):
		ingestJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "album_review_limit"})
	case errors.Is(err, models.ErrSourceGalleryConflict), errors.Is(err, models.ErrSourceAttachmentConflict), errors.Is(err, models.ErrAttachmentSelectionConflict), errors.Is(err, models.ErrSourcePostForgotten):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "album_preview_changed"})
	case errors.Is(err, models.ErrArchiveJobConflict), errors.Is(err, models.ErrArchiveJobLease):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "album_job_changed"})
	default:
		ingestError(w, err)
	}
}

func writeAlbumStatus(w http.ResponseWriter, status *gallery.AlbumBackfillStatus, err error, admitted bool) {
	if err != nil {
		albumError(w, err)
		return
	}
	code := http.StatusOK
	if admitted && (status.State == "queued" || status.State == "running") {
		code = http.StatusAccepted
	}
	ingestJSON(w, code, status)
}

func (rs *nativeArchiveRoutes) previewAlbum(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Policy string `json:"policy"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := rs.albumService().Preview(r.Context(), chi.URLParam(r, "post"), input.Policy)
	if err != nil {
		albumError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyAlbum(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RequestUUID string `json:"request_uuid"`
		Policy      string `json:"policy"`
		Signature   string `json:"signature"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := rs.albumService().Submit(r.Context(), gallery.AlbumBackfillRequest{RequestUUID: input.RequestUUID, PostUUID: chi.URLParam(r, "post"), Policy: input.Policy, Signature: input.Signature})
	writeAlbumStatus(w, result, err, true)
}

func (rs *nativeArchiveRoutes) albumJob(w http.ResponseWriter, r *http.Request) {
	result, err := rs.albumService().Status(r.Context(), chi.URLParam(r, "job"), false)
	writeAlbumStatus(w, result, err, false)
}

func (rs *nativeArchiveRoutes) albumRequest(w http.ResponseWriter, r *http.Request) {
	result, err := rs.albumService().Status(r.Context(), chi.URLParam(r, "request"), true)
	writeAlbumStatus(w, result, err, false)
}

func albumPage(r *http.Request) (int64, int, error) {
	after, limit := int64(0), 50
	var err error
	if value := r.URL.Query().Get("after"); value != "" {
		after, err = strconv.ParseInt(value, 10, 64)
	}
	if err == nil {
		if value := r.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
		}
	}
	if err != nil || after < 0 || limit < 1 || limit > 100 {
		return 0, 0, ingest.ErrInvalid
	}
	return after, limit, nil
}

func (rs *nativeArchiveRoutes) albumHistory(w http.ResponseWriter, r *http.Request) {
	after, limit, err := albumPage(r)
	if err != nil {
		albumError(w, err)
		return
	}
	result, err := rs.albumService().History(r.Context(), chi.URLParam(r, "post"), after, limit)
	if err != nil {
		albumError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) albumAttempts(w http.ResponseWriter, r *http.Request) {
	after, limit, err := albumPage(r)
	if err != nil {
		albumError(w, err)
		return
	}
	result, err := rs.albumService().Attempts(r.Context(), chi.URLParam(r, "job"), after, limit)
	if err != nil {
		albumError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) cancelAlbum(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision int64 `json:"expected_revision"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := rs.albumService().Cancel(r.Context(), chi.URLParam(r, "job"), input.Revision)
	writeAlbumStatus(w, result, err, false)
}

func (rs *nativeArchiveRoutes) retryAlbum(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RequestUUID string `json:"request_uuid"`
		Revision    int64  `json:"expected_revision"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := rs.albumService().Retry(r.Context(), chi.URLParam(r, "job"), input.Revision, input.RequestUUID)
	writeAlbumStatus(w, result, err, true)
}
