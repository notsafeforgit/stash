package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

func validatePostConsolidationReviewSchema(conn *sqlx.DB) error {
	for _, name := range []string{"post_consolidation_reviews", "post_consolidation_review_members", "post_consolidation_review_media", "post_consolidation_review_attachments",
		"post_consolidation_review_immutable", "post_consolidation_review_member_immutable", "post_consolidation_review_media_immutable", "post_consolidation_review_attachment_immutable",
		"post_consolidation_review_scope", "post_consolidation_review_member_scope", "post_consolidation_review_media_scope", "post_consolidation_review_attachment_scope", "archive_jobs_post_merge_notify", "archive_jobs_work_history"} {
		var present bool
		if err := conn.Get(&present, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !present {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	for after := ""; ; {
		var id string
		if err := conn.Get(&id, "SELECT coalesce(min(request_uuid),'') FROM post_consolidation_reviews WHERE request_uuid>?", after); err != nil {
			return err
		}
		if id == "" {
			break
		}
		if _, err := readPostConsolidationReview(conn.Get, conn.Select, id); err != nil {
			return fmt.Errorf("post merge review %s: %w", id, err)
		}
		after = id
	}
	for after := ""; ; {
		var row archiveJobRow
		err := conn.Get(&row, "SELECT * FROM archive_jobs INDEXED BY archive_jobs_post_merge_notify WHERE kind='post.merge_notify' AND uuid>? ORDER BY uuid LIMIT 1", after)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return err
		}
		work, err := models.ParsePostConsolidationNotification([]byte(row.Arguments))
		if err != nil {
			return models.ErrSourcePayloadCorrupt
		}
		review, err := readPostConsolidationReview(conn.Get, conn.Select, work.ReviewUUID)
		if err != nil {
			return err
		}
		if review == nil || !review.Result.Gallery.Changed() {
			return models.ErrSourcePayloadCorrupt
		}
		if err := validatePostMergeNotificationRetry(conn.Get, row.resolve(), review); err != nil {
			return err
		}
		after = row.UUID
	}
	return nil
}
