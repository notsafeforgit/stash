package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateMetadataFileReviewSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"metadata_file_edit_reviews", "metadata_file_edit_reviews_history", "metadata_file_edit_review_immutable", "metadata_file_edit_review_scope"} {
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
		if err := conn.Get(&id, "SELECT coalesce(min(request_uuid),'') FROM metadata_file_edit_reviews WHERE request_uuid>?", after); err != nil || id == "" {
			return err
		}
		if _, err := readMetadataFileReview(conn.Get, id); err != nil {
			return fmt.Errorf("historical metadata review %s: %w", id, err)
		}
		after = id
	}
}
