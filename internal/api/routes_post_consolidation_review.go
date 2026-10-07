package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
)

func postConsolidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrPostConsolidationReviewInvalid), errors.Is(err, models.ErrSourcePostIdentityInvalid),
		errors.Is(err, models.ErrSourcePostComparisonInvalid), errors.Is(err, gallery.ErrPostMergeNotificationInvalid):
		ingestError(w, ingest.ErrInvalid)
	case errors.Is(err, models.ErrSourcePostComparisonMissing), errors.Is(err, gallery.ErrPostMergeNotificationNotFound):
		ingestError(w, ingest.ErrNotFound)
	case errors.Is(err, models.ErrSourcePostIdentityLimit), errors.Is(err, models.ErrSourcePostComparisonLimit), errors.Is(err, models.ErrSourceAlbumLimit):
		ingestJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "post_comparison_limit", "message": "The posts exceed the bounded review limit. No choices have been omitted or changed."})
	case errors.Is(err, models.ErrSourcePostIdentifierConflict):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "post_identity_conflict", "message": "These have different source post identifiers and must remain separate posts."})
	case errors.Is(err, models.ErrSourcePostConsolidationReplay):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "request_conflict", "message": "This request UUID already names a different post merge."})
	case errors.Is(err, models.ErrPostConsolidationReviewConflict), errors.Is(err, models.ErrSourcePostIdentityConflict):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "preview_changed", "message": "The posts or their choices changed; review a fresh preview."})
	case errors.Is(err, models.ErrArchiveJobConflict), errors.Is(err, models.ErrArchiveJobLease):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "notification_job_changed", "message": "The notification job or saved retry changed; refresh its status."})
	default:
		nativeArchiveError(w, err)
	}
}

func (rs *nativeArchiveRoutes) previewPostConsolidation(w http.ResponseWriter, r *http.Request) {
	var input models.PostConsolidationReviewInput
	if err := readIngestJSON(w, r, models.MaxPostConsolidationReviewBytes, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.PostConsolidationReviewPreview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceEvidence.PreviewConsolidationReview(ctx, input)
		return err
	})
	if err != nil {
		postConsolidationError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) applyPostConsolidation(w http.ResponseWriter, r *http.Request) {
	var input models.PostConsolidationReviewApplyInput
	if err := readIngestJSON(w, r, models.MaxPostConsolidationReviewBytes, &input); err != nil {
		ingestError(w, err)
		return
	}
	var result *models.PostConsolidationReview
	var replayed bool
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, replayed, err = rs.repo.SourceEvidence.ApplyConsolidationReview(ctx, input, time.Now().UTC())
		return err
	})
	if err != nil {
		postConsolidationError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]any{"review": result, "replayed": replayed})
}

// A saved request can be checked without publishing or re-previewing its choices.
func (rs *nativeArchiveRoutes) checkPostConsolidation(w http.ResponseWriter, r *http.Request) {
	var input models.PostConsolidationReviewApplyInput
	if err := readIngestJSON(w, r, models.MaxPostConsolidationReviewBytes, &input); err != nil {
		ingestError(w, err)
		return
	}
	if input.RequestUUID != chi.URLParam(r, "request") {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.PostConsolidationReview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceEvidence.CheckConsolidationReview(ctx, input)
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	if err != nil {
		postConsolidationError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) postConsolidationReview(w http.ResponseWriter, r *http.Request) {
	var result *models.PostConsolidationReview
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceEvidence.ConsolidationReview(ctx, chi.URLParam(r, "request"))
		if err == nil && result == nil {
			err = ingest.ErrNotFound
		}
		return err
	})
	if err != nil {
		postConsolidationError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) postConsolidationHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "post")
	cursor, limit, err := accountReviewPage(r)
	after := 0
	if cursor != "" && err == nil {
		after, err = strconv.Atoi(cursor)
	}
	if err != nil || after < 0 || !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.SourcePostConsolidation
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		post, err := rs.repo.SourceEvidence.PostIdentity(ctx, id)
		if err != nil {
			return err
		}
		if post == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.SourceEvidence.PostConsolidationHistory(ctx, id, after, limit)
		return err
	})
	if err != nil {
		postConsolidationError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func writePostMergeNotification(w http.ResponseWriter, job *models.ArchiveJob, err error, admitted bool) {
	if err != nil {
		postConsolidationError(w, err)
		return
	}
	result, err := postMergeNotificationView(job)
	if err != nil {
		postConsolidationError(w, err)
		return
	}
	code := http.StatusOK
	if admitted && (job.State == "queued" || job.State == "running") {
		code = http.StatusAccepted
	}
	ingestJSON(w, code, result)
}

func postMergeNotificationView(job *models.ArchiveJob) (map[string]any, error) {
	work, err := models.ParsePostConsolidationNotification(job.Arguments)
	if err != nil {
		return nil, err
	}
	return map[string]any{"sequence": job.Sequence, "job_uuid": job.UUID, "review_uuid": work.ReviewUUID, "state": job.State, "revision": job.Revision,
		"resume_from_job_uuid": work.ResumeFromJobUUID, "resume_from_job_revision": work.ResumeFromJobRevision,
		"hooks_finished": job.State == "succeeded", "error_code": job.ErrorCode, "created_at": job.CreatedAt, "updated_at": job.UpdatedAt}, nil
}

func (rs *nativeArchiveRoutes) postMergeNotificationHistory(w http.ResponseWriter, r *http.Request) {
	before, limit := int64(0), 25
	var err error
	if value := r.URL.Query().Get("before"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
	}
	if err == nil {
		if value := r.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
		}
	}
	if err != nil || before < 0 || limit < 1 || limit > 100 {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	jobs, err := gallery.NewPostMergeNotifications(rs.repo).History(r.Context(), chi.URLParam(r, "request"), before, limit)
	if err != nil {
		postConsolidationError(w, err)
		return
	}
	result := make([]map[string]any, 0, len(jobs))
	for i := range jobs {
		item, err := postMergeNotificationView(&jobs[i])
		if err != nil {
			postConsolidationError(w, err)
			return
		}
		result = append(result, item)
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) postMergeNotification(w http.ResponseWriter, r *http.Request) {
	job, err := gallery.NewPostMergeNotifications(rs.repo).Find(r.Context(), chi.URLParam(r, "job"))
	writePostMergeNotification(w, job, err, false)
}

func (rs *nativeArchiveRoutes) postMergeNotificationRequest(w http.ResponseWriter, r *http.Request) {
	job, err := gallery.NewPostMergeNotifications(rs.repo).FindRequest(r.Context(), chi.URLParam(r, "request"))
	writePostMergeNotification(w, job, err, false)
}

func (rs *nativeArchiveRoutes) cancelPostMergeNotification(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision int64 `json:"expected_revision"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	job, err := gallery.NewPostMergeNotifications(rs.repo).Cancel(r.Context(), chi.URLParam(r, "job"), input.Revision)
	writePostMergeNotification(w, job, err, false)
}

func (rs *nativeArchiveRoutes) retryPostMergeNotification(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RequestUUID string `json:"request_uuid"`
		Revision    int64  `json:"expected_revision"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	job, err := gallery.NewPostMergeNotifications(rs.repo).Retry(r.Context(), input.RequestUUID, chi.URLParam(r, "job"), input.Revision)
	writePostMergeNotification(w, job, err, true)
}
