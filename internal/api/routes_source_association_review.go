package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func sourceAssociationReviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrSourceAssociationReviewInvalid):
		ingestError(w, ingest.ErrInvalid)
	case errors.Is(err, models.ErrSourceAssociationReviewReplay):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "request_conflict", "message": "This request UUID already names a different source association choice."})
	case errors.Is(err, models.ErrSourceAssociationReviewConflict), errors.Is(err, models.ErrSourceGalleryConflict), errors.Is(err, models.ErrSourceAttachmentConflict),
		errors.Is(err, models.ErrSourcePostMediaConflict), errors.Is(err, models.ErrSourcePostForgotten):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "preview_changed", "message": "The source or library association changed; load a fresh preview."})
	default:
		nativeArchiveError(w, err)
	}
}

func (rs *nativeArchiveRoutes) previewGalleryAssociation(w http.ResponseWriter, r *http.Request) {
	var input models.GalleryAssociationReviewInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.GalleryAssociationReviewPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceGallery.PreviewAssociationReview(ctx, input)
		return err
	})
	if err != nil {
		sourceAssociationReviewError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyGalleryAssociation(w http.ResponseWriter, r *http.Request) {
	var input models.GalleryAssociationReviewApplyInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.GalleryAssociationReview
	var replayed bool
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, replayed, err = rs.repo.SourceGallery.ApplyAssociationReview(ctx, input)
		return err
	})
	if err != nil {
		sourceAssociationReviewError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"review": result, "replayed": replayed})
}

func (rs *nativeArchiveRoutes) galleryAssociationReview(w http.ResponseWriter, r *http.Request) {
	var result *models.GalleryAssociationReview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceGallery.AssociationReview(ctx, chi.URLParam(r, "request"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	if err != nil {
		sourceAssociationReviewError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func sourceAssociationHistoryPage(r *http.Request) (int, int, error) {
	cursor, limit, err := accountReviewPage(r)
	after := 0
	if cursor != "" && err == nil {
		after, err = strconv.Atoi(cursor)
	}
	if err != nil || after < 0 {
		return 0, 0, ingest.ErrInvalid
	}
	return after, limit, nil
}

func (rs *nativeArchiveRoutes) galleryAssociationHistory(w http.ResponseWriter, r *http.Request) {
	post := chi.URLParam(r, "post")
	after, limit, err := sourceAssociationHistoryPage(r)
	if err != nil || !ingest.ValidUUID(post) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	result := []models.SourcePostAlbum{}
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		found, err := rs.repo.SourceEvidence.FindPost(ctx, post)
		if err != nil {
			return err
		}
		if found == nil {
			return ingest.ErrNotFound
		}
		decisions, err := rs.repo.SourceGallery.AssociationHistory(ctx, post, after, limit)
		if err != nil {
			return err
		}
		for _, d := range decisions {
			result = append(result, models.SourcePostAlbum{PostUUID: d.PostUUID, DecisionUUID: d.UUID, Revision: d.Revision,
				State: d.State, GalleryUUID: d.GalleryUUID, SelectionUUID: d.SelectionUUID, Origin: d.Origin, Reason: d.Reason, CreatedAt: d.CreatedAt})
		}
		return nil
	})
	if err != nil {
		sourceAssociationReviewError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) attachmentMediaContext(w http.ResponseWriter, r *http.Request) {
	var result *models.AttachmentMediaReviewContext
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAttachment.MediaReviewContext(ctx, chi.URLParam(r, "attachment"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	if err != nil {
		sourceAssociationReviewError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) attachmentMediaHistory(w http.ResponseWriter, r *http.Request) {
	attachment := chi.URLParam(r, "attachment")
	after, limit, err := sourceAssociationHistoryPage(r)
	if err != nil || !ingest.ValidUUID(attachment) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.AttachmentMediaReviewDecision
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		found, err := rs.repo.SourceAttachment.Find(ctx, attachment)
		if err != nil {
			return err
		}
		if found == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceAttachment.MediaReviewHistory(ctx, attachment, after, limit)
		return err
	})
	if err != nil {
		sourceAssociationReviewError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) previewAttachmentMedia(w http.ResponseWriter, r *http.Request) {
	var input models.AttachmentMediaReviewInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.AttachmentMediaReviewPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAttachment.PreviewMediaReview(ctx, input)
		return err
	})
	if err != nil {
		sourceAssociationReviewError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyAttachmentMedia(w http.ResponseWriter, r *http.Request) {
	var input models.AttachmentMediaReviewApplyInput
	if err := readIngestJSON(w, r, 16384, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.AttachmentMediaReview
	var replayed bool
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, replayed, err = rs.repo.SourceAttachment.ApplyMediaReview(ctx, input)
		return err
	})
	if err != nil {
		sourceAssociationReviewError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"review": result, "replayed": replayed})
}

func (rs *nativeArchiveRoutes) attachmentMediaReview(w http.ResponseWriter, r *http.Request) {
	var result *models.AttachmentMediaReview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceAttachment.MediaReview(ctx, chi.URLParam(r, "request"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	if err != nil {
		sourceAssociationReviewError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
