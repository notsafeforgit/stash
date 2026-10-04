package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateAccountReviewSchema(conn *sqlx.DB) error {
	for _, name := range []string{"account_ownership_reviews", "account_ownership_reviews_account", "account_ownership_review_immutable", "account_ownership_review_scope"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	for after := ""; ; {
		var id string
		if err := conn.Get(&id, "SELECT coalesce(min(request_uuid),'') FROM account_ownership_reviews WHERE request_uuid>?", after); err != nil || id == "" {
			return err
		}
		if _, err := readAccountOwnershipReview(conn.Get, id); err != nil {
			return fmt.Errorf("account ownership review %s: %w", id, err)
		}
		after = id
	}
}
