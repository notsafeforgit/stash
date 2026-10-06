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

func (rs *nativeArchiveRoutes) previewAttachmentSelection(w http.ResponseWriter, r *http.Request) {
	var input models.AttachmentSelectionReviewInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.AttachmentSelectionReviewPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAttachment.PreviewSelectionReview(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyAttachmentSelection(w http.ResponseWriter, r *http.Request) {
	var input models.AttachmentSelectionReviewApplyInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.AttachmentSelectionReview
	var replayed bool
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, replayed, err = rs.repo.SourceAttachment.ApplySelectionReview(ctx, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"review": result, "replayed": replayed})
}

func (rs *nativeArchiveRoutes) attachmentSelectionReview(w http.ResponseWriter, r *http.Request) {
	var result *models.AttachmentSelectionReview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAttachment.SelectionReview(ctx, chi.URLParam(r, "request"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) attachmentSelectionManifests(w http.ResponseWriter, r *http.Request) {
	post := chi.URLParam(r, "post")
	after, limit, err := accountReviewPage(r)
	if err != nil || !ingest.ValidUUID(post) || (after != "" && !ingest.ValidUUID(after)) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.AttachmentSelectionReviewManifest
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		found, err := rs.repo.SourceEvidence.FindPost(ctx, post)
		if err != nil {
			return err
		}
		if found == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceAttachment.ReviewSelectionManifests(ctx, post, after, limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

type attachmentSelectionReviewDecision struct {
	UUID          string    `json:"uuid"`
	PostUUID      string    `json:"post_uuid"`
	Revision      int       `json:"revision"`
	Mode          string    `json:"mode"`
	Origin        string    `json:"origin"`
	Reason        string    `json:"reason"`
	CaptureUUID   *string   `json:"capture_uuid"`
	ManifestUUIDs []string  `json:"manifest_uuids"`
	CreatedAt     time.Time `json:"created_at"`
}

func (rs *nativeArchiveRoutes) attachmentSelectionHistory(w http.ResponseWriter, r *http.Request) {
	post := chi.URLParam(r, "post")
	cursor, limit, err := accountReviewPage(r)
	after := 0
	if cursor != "" && err == nil {
		after, err = strconv.Atoi(cursor)
	}
	if err != nil || after < 0 || !ingest.ValidUUID(post) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	result := []attachmentSelectionReviewDecision{}
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		found, err := rs.repo.SourceEvidence.FindPost(ctx, post)
		if err != nil {
			return err
		}
		if found == nil {
			return ingest.ErrNotFound
		}
		decisions, err := rs.repo.SourceAttachment.SelectionHistory(ctx, post, after, limit)
		if err != nil {
			return err
		}
		for _, d := range decisions {
			result = append(result, attachmentSelectionReviewDecision{UUID: d.UUID, PostUUID: d.PostUUID, Revision: d.Revision,
				Mode: d.Mode, Origin: d.Origin, Reason: d.Reason, CaptureUUID: d.CaptureUUID,
				ManifestUUIDs: append([]string{}, d.ManifestUUIDs...), CreatedAt: d.CreatedAt})
		}
		return nil
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
