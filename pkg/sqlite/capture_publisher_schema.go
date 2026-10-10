package sqlite

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateCapturePublisherSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"source_account_identifiers_canonical_kind", "capture_publisher_decisions", "capture_publisher_decisions_account", "capture_publisher_heads",
		"capture_publisher_decision_scope", "capture_publisher_decision_publish", "capture_publisher_decision_immutable", "capture_publisher_head_scope", "capture_publisher_head_forward",
		"capture_publisher_claims", "capture_publisher_claims_identifier", "capture_publisher_claim_scope", "capture_publisher_claim_immutable", "capture_publisher_write_context"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var unfinished bool
	if err := conn.Get(&unfinished, "SELECT EXISTS(SELECT 1 FROM capture_publisher_write_context)"); err != nil {
		return err
	}
	if unfinished {
		return errors.New("native database has an unfinished capture publisher write")
	}
	if !auditData {
		return nil
	}
	if err := conn.Get(&unfinished, `SELECT EXISTS(SELECT 1 FROM capture_publisher_decisions d
LEFT JOIN capture_publisher_heads h ON h.capture_uuid=d.capture_uuid
LEFT JOIN capture_publisher_decisions current ON current.uuid=h.decision_uuid
WHERE h.capture_uuid IS NULL OR current.revision<d.revision)`); err != nil {
		return err
	}
	if unfinished {
		return errors.New("native capture publisher head differs from its decision history")
	}
	return nil
}

func validateCapturePublisherCommit(ctx context.Context) error {
	var unfinished bool
	if err := dbWrapper.Get(ctx, &unfinished, "SELECT EXISTS(SELECT 1 FROM capture_publisher_write_context)"); err != nil {
		return err
	}
	if unfinished {
		return errors.New("refusing to commit unfinished capture publisher write")
	}
	return nil
}
