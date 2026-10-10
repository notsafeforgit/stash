package sqlite

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPostMergeNotificationsRejectSubmissionsOutsideOriginalReview(t *testing.T) {
	_, repo, review := postMergeNotificationFixture(t)
	service := gallery.NewPostMergeNotifications(repo)
	parent, err := service.Find(t.Context(), review.Result.NotificationJobUUID)
	require.NoError(t, err)
	parent, err = service.Cancel(t.Context(), parent.UUID, parent.Revision)
	require.NoError(t, err)
	before := identityRows(t, repo, "archive_jobs", "archive_job_submissions")
	for _, test := range []string{"another original", "unknown review", "wrong parent revision"} {
		t.Run(test, func(t *testing.T) {
			input, err := postMergeNotificationSubmission(review.Request.RequestUUID, review.Result.Gallery.GalleryUUID, uuid.NewString())
			require.NoError(t, err)
			work := models.PostConsolidationNotification{Version: 1, ReviewUUID: review.Request.RequestUUID}
			switch test {
			case "unknown review":
				work.ReviewUUID = uuid.NewString()
			case "wrong parent revision":
				work.ResumeFromJobUUID, work.ResumeFromJobRevision = parent.UUID, parent.Revision+1
			}
			input.Arguments, err = work.Arguments()
			require.NoError(t, err)
			err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := repo.ArchiveJob.Submit(ctx, input, time.Now().UTC(), 4096)
				return err
			})
			require.Error(t, err, "a queue write cannot bypass the merge receipt scope")
			require.Equal(t, before, identityRows(t, repo, "archive_jobs", "archive_job_submissions"))
		})
	}
}

func TestPostConsolidationReviewAnonymiseRemovesRequestsChoicesAndNotifications(t *testing.T) {
	db, _, _ := postMergeNotificationFixture(t)
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	require.NoError(t, db.Anonymise(output))
	other := NewDatabase()
	require.NoError(t, other.Open(output))
	defer other.Close()
	for table, rows := range identityRows(t, other.Repository(), "post_consolidation_reviews", "post_consolidation_review_members",
		"post_consolidation_review_media", "post_consolidation_review_attachments", "archive_jobs", "archive_job_submissions", "source_post_consolidations") {
		require.Empty(t, rows, table)
	}
}

func TestPostConsolidationReviewAuditRejectsCorruptReceiptsWithoutWriting(t *testing.T) {
	for _, corrupt := range []string{"gallery scope", "member missing", "retry revision"} {
		t.Run(corrupt, func(t *testing.T) {
			db, repo, review := postMergeNotificationFixture(t)
			var retry *models.ArchiveJob
			if corrupt == "retry revision" {
				service := gallery.NewPostMergeNotifications(repo)
				parent, err := service.Find(t.Context(), review.Result.NotificationJobUUID)
				require.NoError(t, err)
				parent, err = service.Cancel(t.Context(), parent.UUID, parent.Revision)
				require.NoError(t, err)
				retry, err = service.Retry(t.Context(), uuid.NewString(), parent.UUID, parent.Revision)
				require.NoError(t, err)
			}
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				if corrupt == "member missing" {
					_, err := dbWrapper.Exec(ctx, "DELETE FROM post_consolidation_review_members WHERE request_uuid=? AND post_uuid=?", review.Request.RequestUUID, review.Request.SourceUUID)
					return err
				}
				trigger := "post_consolidation_review_immutable"
				if corrupt == "retry revision" {
					trigger = "archive_job_identity"
				}
				var definition string
				if err := dbWrapper.Get(ctx, &definition, "SELECT sql FROM sqlite_schema WHERE name=?", trigger); err != nil {
					return err
				}
				if _, err := dbWrapper.Exec(ctx, "DROP TRIGGER "+trigger); err != nil {
					return err
				}
				if corrupt == "retry revision" {
					_, err := dbWrapper.Exec(ctx, "UPDATE archive_jobs SET arguments=json_set(arguments,'$.resume_from_job_revision',999),revision=revision+1 WHERE uuid=?", retry.UUID)
					if err != nil {
						return err
					}
				} else {
					review.Result.Gallery.Action = "disabled"
					body, err := json.Marshal(review.Result)
					if err != nil {
						return err
					}
					signature, err := postConsolidationReceiptSignature(review)
					if err != nil {
						return err
					}
					if _, err := dbWrapper.Exec(ctx, "UPDATE post_consolidation_reviews SET result_json=?,signature=? WHERE request_uuid=?", string(body), signature, review.Request.RequestUUID); err != nil {
						return err
					}
				}
				_, err := dbWrapper.Exec(ctx, definition)
				return err
			}))
			require.NoError(t, db.Close())
			before, err := os.ReadFile(db.DatabasePath())
			require.NoError(t, err)
			require.Error(t, db.AuditForTesting(db.DatabasePath()))
			after, err := os.ReadFile(db.DatabasePath())
			require.NoError(t, err)
			require.Equal(t, before, after, "invalid recovery evidence must be rejected before any audit write")
		})
	}
}
