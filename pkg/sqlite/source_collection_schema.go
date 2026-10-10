package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateSourceCollectionSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{
		"media_roots", "media_root_revisions", "media_root_revision_scope", "media_root_revision_publish", "media_root_revision_immutable", "media_root_identity_immutable",
		"source_accounts_collection_scope", "source_collections", "source_collection_revisions", "source_collection_target", "source_collection_root", "source_collection_account",
		"source_collection_revision_scope", "source_collection_revision_publish", "source_collection_revision_immutable", "source_collection_identity_immutable",
		"source_collection_captures", "source_collection_captures_capture", "source_collection_capture_immutable",
		"source_collection_media_intake", "source_collection_intake_page", "source_collection_intake_media", "source_collection_intake_kind", "source_collection_intake_immutable",
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
	if !auditData {
		return nil
	}
	if err := conn.Get(&unfinished, `SELECT EXISTS(SELECT 1 FROM media_roots b LEFT JOIN media_root_revisions r ON r.root_uuid=b.uuid AND r.revision=b.revision WHERE r.root_uuid IS NULL)
OR EXISTS(SELECT 1 FROM source_collections b LEFT JOIN source_collection_revisions r ON r.collection_uuid=b.uuid AND r.revision=b.revision WHERE r.collection_uuid IS NULL)`); err != nil {
		return err
	}
	if unfinished {
		return errors.New("native database has an unfinished root or collection definition")
	}
	return nil
}
