package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateMetadataPolicySchema(conn *sqlx.DB) error {
	for _, name := range []string{
		"metadata_policies", "metadata_policy_revisions", "metadata_policy_revision_scope", "metadata_policy_revision_publish",
		"metadata_policy_revision_immutable", "metadata_policy_identity_immutable", "metadata_decision_policies", "metadata_decision_policy_history",
		"metadata_decision_policy_immutable", "metadata_decision_policy_origin", "media_root_path", "source_collection_directory",
	} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var unfinished bool
	if err := conn.Get(&unfinished, `SELECT EXISTS(SELECT 1 FROM metadata_policies p LEFT JOIN metadata_policy_revisions r
ON r.collection_uuid=p.collection_uuid AND r.revision=p.revision WHERE r.collection_uuid IS NULL)`); err != nil {
		return err
	}
	if unfinished {
		return errors.New("native database has an unfinished metadata policy")
	}
	return nil
}
