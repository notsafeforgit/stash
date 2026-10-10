package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validatePostSelectionProvenanceSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"post_attachment_decision_capture_scope", "post_attachment_decision_manifest_scope"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=? AND type='trigger')", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native post selection schema is incomplete: missing %s", name)
		}
	}
	if !auditData {
		return nil
	}
	for _, query := range []string{
		`SELECT EXISTS(SELECT 1 FROM post_attachment_decisions d
LEFT JOIN source_captures c ON c.uuid=d.capture_uuid
LEFT JOIN source_post_identities owner ON owner.post_uuid=c.post_uuid
LEFT JOIN source_post_identities selected ON selected.post_uuid=d.post_uuid
WHERE d.capture_uuid IS NOT NULL AND
 (owner.canonical_uuid IS NULL OR selected.canonical_uuid IS NULL OR owner.canonical_uuid!=selected.canonical_uuid))`,
		`SELECT EXISTS(SELECT 1 FROM post_attachment_decision_manifests d
LEFT JOIN source_attachment_manifests m ON m.uuid=d.manifest_uuid
LEFT JOIN source_post_identities owner ON owner.post_uuid=m.post_uuid
LEFT JOIN source_post_identities selected ON selected.post_uuid=d.post_uuid
WHERE owner.canonical_uuid IS NULL OR selected.canonical_uuid IS NULL OR owner.canonical_uuid!=selected.canonical_uuid)`,
	} {
		var invalid bool
		if err := conn.Get(&invalid, query); err != nil {
			return err
		}
		if invalid {
			return errors.New("native post selection evidence belongs to another post identity")
		}
	}
	return nil
}
