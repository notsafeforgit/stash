package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

func postMergeNotificationSubmission(review, gallery, request string) (models.ArchiveJobSubmission, error) {
	var ret models.ArchiveJobSubmission
	if !validSourceRunUUID(review) || !validSourceRunUUID(gallery) || !validSourceRunUUID(request) {
		return ret, models.ErrPostConsolidationReviewInvalid
	}
	// Archive job arguments use canonical object key ordering. Construct the
	// work digest from those same bytes so persisted jobs validate after reopen.
	arguments, err := (models.PostConsolidationNotification{Version: 1, ReviewUUID: review}).Arguments()
	if err != nil {
		return ret, err
	}
	work, err := sourceSignature("stash-post-merge-notification-v1", json.RawMessage(arguments))
	if err != nil {
		return ret, err
	}
	resource, err := sourceSignature("stash-gallery-notification-resource-v1", gallery)
	if err != nil {
		return ret, err
	}
	return models.ArchiveJobSubmission{RequestUUID: request, Kind: models.ArchiveJobNotifyPostMerge, WorkKey: work, ResourceKey: resource,
		Arguments: arguments, Priority: 50, MaxAttempts: 8}, nil
}

func submitPostMergeNotification(ctx context.Context, review, gallery, request string, now time.Time) (*models.ArchiveJob, error) {
	input, err := postMergeNotificationSubmission(review, gallery, request)
	if err != nil {
		return nil, err
	}
	return (&ArchiveJobStore{}).Submit(ctx, input, now, 4096)
}

func validatePostMergeNotificationJob(job *models.ArchiveJob, review, gallery string) error {
	if job == nil || job.Kind != models.ArchiveJobNotifyPostMerge {
		return models.ErrSourcePayloadCorrupt
	}
	want, err := postMergeNotificationSubmission(review, gallery, job.UUID)
	if err != nil || string(job.Arguments) != string(want.Arguments) || job.WorkKey != want.WorkKey || job.ResourceKey != want.ResourceKey || job.MaxAttempts != want.MaxAttempts {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}

func validatePostMergeNotificationRetry(get enrichmentGet, job *models.ArchiveJob, review *models.PostConsolidationReview) error {
	if job.UUID == review.Result.NotificationJobUUID {
		return validatePostMergeNotificationJob(job, review.Request.RequestUUID, review.Result.Gallery.GalleryUUID)
	}
	work, err := models.ParsePostConsolidationNotification(job.Arguments)
	if err != nil || work.ReviewUUID != review.Request.RequestUUID || work.ResumeFromJobUUID == "" || work.ResumeFromJobUUID == job.UUID {
		return models.ErrSourcePayloadCorrupt
	}
	want, err := postMergeNotificationSubmission(work.ReviewUUID, review.Result.Gallery.GalleryUUID, job.UUID)
	if err != nil || job.Kind != want.Kind || job.WorkKey != want.WorkKey || job.ResourceKey != want.ResourceKey || job.MaxAttempts != want.MaxAttempts {
		return models.ErrSourcePayloadCorrupt
	}
	var row archiveJobRow
	if err := get(&row, "SELECT * FROM archive_jobs WHERE uuid=?", work.ResumeFromJobUUID); err != nil {
		return err
	}
	parent := row.resolve()
	prior, err := models.ParsePostConsolidationNotification(parent.Arguments)
	if err != nil || prior.ReviewUUID != work.ReviewUUID || parent.Kind != job.Kind || parent.WorkKey != job.WorkKey ||
		parent.ResourceKey != job.ResourceKey || parent.MaxAttempts != job.MaxAttempts || parent.Sequence >= job.Sequence ||
		parent.Revision != work.ResumeFromJobRevision || (parent.State != "failed" && parent.State != "cancelled") ||
		(prior.ResumeFromJobUUID == "" && parent.UUID != review.Result.NotificationJobUUID) {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}

func postMergeNotificationSubmissionGuard(ctx context.Context, request string) {
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		job, err := (&ArchiveJobStore{}).FindSubmission(ctx, request)
		if err != nil {
			return err
		}
		if job == nil {
			return models.ErrArchiveJobConflict
		}
		work, err := models.ParsePostConsolidationNotification(job.Arguments)
		if err != nil {
			return models.ErrPostConsolidationReviewInvalid
		}
		review, err := (&SourceEvidenceStore{}).ConsolidationReview(ctx, work.ReviewUUID)
		if err != nil {
			return err
		}
		if review == nil || !review.Result.Gallery.Changed() {
			return models.ErrPostConsolidationReviewInvalid
		}
		return validatePostMergeNotificationRetry(func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) }, job, review)
	})
}
