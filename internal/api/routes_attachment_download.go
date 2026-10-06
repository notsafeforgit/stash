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

func (rs *nativeArchiveRoutes) attachmentDownloadHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "attachment")
	cursor, limit, err := accountReviewPage(r)
	after := int64(0)
	if cursor != "" && err == nil {
		after, err = strconv.ParseInt(cursor, 10, 64)
	}
	if err != nil || after < 0 || !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.AttachmentDownloadReport
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		attachment, err := rs.repo.SourceAttachment.Find(ctx, id)
		if err != nil {
			return err
		}
		if attachment == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceAttachment.DownloadHistory(ctx, id, after, limit, time.Now())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
