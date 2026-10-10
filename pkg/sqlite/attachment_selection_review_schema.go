package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateAttachmentSelectionReviewSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"attachment_selection_reviews", "attachment_selection_reviews_post", "attachment_selection_review_immutable", "attachment_selection_review_scope"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	if !auditData {
		return nil
	}
	for after := ""; ; {
		var id string
		if err := conn.Get(&id, "SELECT coalesce(min(request_uuid),'') FROM attachment_selection_reviews WHERE request_uuid>?", after); err != nil || id == "" {
			return err
		}
		if _, err := readAttachmentSelectionReview(conn.Get, id); err != nil {
			return fmt.Errorf("source-list selection review %s: %w", id, err)
		}
		after = id
	}
}
