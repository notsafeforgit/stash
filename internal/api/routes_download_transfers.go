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

func (rs *nativeArchiveRoutes) attachmentDownloadTransfers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "attachment")
	values := r.URL.Query()
	before := int64(0)
	limit := 25
	valid := ingest.ValidUUID(id)
	for key, list := range values {
		if len(list) != 1 || (key != "before" && key != "limit") {
			valid = false
			break
		}
		n, err := strconv.ParseInt(list[0], 10, 64)
		if err != nil || n < 1 || n > 9007199254740991 || strconv.FormatInt(n, 10) != list[0] {
			valid = false
			break
		}
		switch {
		case key == "before":
			before = n
		case n > 25:
			valid = false
		default:
			limit = int(n)
		}
	}
	if !valid {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.AttachmentDownloadPage
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		attachment, err := rs.repo.SourceAttachment.Find(ctx, id)
		if err != nil {
			return err
		}
		if attachment == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceAttachment.DownloadTransfers(ctx, id, before, limit, time.Now().UTC())
		return err
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

// Batch only the selected page's distinct attachments. This is a summary of
// most recently recorded transfers, not a count of active transfers or a claim
// that an attachment is available. Earlier source attempts remain in history.
func (rs *nativeArchiveRoutes) attachmentDownloadStatus(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Attachments []string `json:"attachments"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	if len(input.Attachments) < 1 || len(input.Attachments) > 25 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	seen := make(map[string]bool, len(input.Attachments))
	for _, id := range input.Attachments {
		if !ingest.ValidUUID(id) || seen[id] {
			ingestError(w, ingest.ErrInvalid)
			return
		}
		seen[id] = true
	}
	type summary struct {
		AttachmentUUID string                             `json:"attachment_uuid"`
		Latest         *models.AttachmentDownloadTransfer `json:"latest"`
	}
	result := struct {
		CheckedAt   time.Time `json:"checked_at"`
		Attachments []summary `json:"attachments"`
	}{CheckedAt: time.Now().UTC(), Attachments: []summary{}}
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		for _, id := range input.Attachments {
			attachment, err := rs.repo.SourceAttachment.Find(ctx, id)
			if err != nil {
				return err
			}
			if attachment == nil {
				return ingest.ErrNotFound
			}
			page, err := rs.repo.SourceAttachment.DownloadTransfers(ctx, id, 0, 1, result.CheckedAt)
			if err != nil {
				return err
			}
			entry := summary{AttachmentUUID: id}
			if len(page.Transfers) > 0 {
				entry.Latest = &page.Transfers[0]
			}
			result.Attachments = append(result.Attachments, entry)
		}
		return nil
	})
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
