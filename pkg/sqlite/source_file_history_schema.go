package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

func validateSourceFileHistorySchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"source_file_history", "source_file_history_locations", "source_file_history_edits", "source_file_history_states", "source_file_history_deduplications",
		"source_file_history_reference", "source_file_history_collection", "source_file_history_observation", "source_file_history_claim",
		"source_file_history_immutable", "source_file_history_location_immutable", "source_file_history_location_scope", "source_file_history_edit_immutable",
		"source_file_history_edit_scope", "source_file_history_state_immutable", "source_file_history_state_scope", "source_file_history_deduplication_immutable", "source_file_history_deduplication_scope"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_file_history h
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=h.collection_uuid AND c.revision=h.collection_revision
 LEFT JOIN media_root_revisions r ON r.root_uuid=h.root_uuid AND r.revision=h.root_revision
 WHERE c.collection_uuid IS NULL OR r.root_uuid IS NULL)`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	for _, table := range []string{"locations", "edits", "states", "deduplications"} {
		if err := conn.Get(&invalid, "SELECT EXISTS(SELECT 1 FROM source_file_history_"+table+" x LEFT JOIN source_file_history h ON h.uuid=x.history_uuid WHERE h.uuid IS NULL)"); err != nil {
			return err
		}
		if invalid {
			return models.ErrSourcePayloadCorrupt
		}
	}
	for after := ""; ; {
		var id string
		if err := conn.Get(&id, "SELECT coalesce(min(uuid),'') FROM source_file_history WHERE uuid>?", after); err != nil {
			return err
		}
		if id == "" {
			break
		}
		if _, err := readSourceFileHistory(conn.Get, conn.Select, id); err != nil {
			return fmt.Errorf("source file history %s: %w", id, err)
		}
		after = id
	}
	return nil
}
