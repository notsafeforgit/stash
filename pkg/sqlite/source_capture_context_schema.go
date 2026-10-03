package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func validateSourceCaptureContextSchema(conn *sqlx.DB) error {
	for _, name := range []string{"source_capture_contexts", "source_capture_contexts_parent", "source_captures_with_context", "source_capture_context_immutable", "source_capture_context_scope"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_capture_contexts x
 LEFT JOIN source_captures c ON c.uuid=x.capture_uuid
 WHERE c.uuid IS NULL OR c.retention_policy!='source-retention-v1+capture-context-v1')
 OR EXISTS(SELECT 1 FROM source_captures c INDEXED BY source_captures_with_context
 WHERE c.retention_policy='source-retention-v1+capture-context-v1'
 AND NOT EXISTS(SELECT 1 FROM source_capture_contexts x WHERE x.capture_uuid=c.uuid))`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	// Each exact embedded object is strictly smaller than its enclosing body;
	// validating every edge also excludes cycles without a library-wide graph.
	after, path := "", ""
	for {
		var links []models.SourceCaptureContext
		if err := conn.Select(&links, "SELECT * FROM source_capture_contexts WHERE (capture_uuid,path)>(?,?) ORDER BY capture_uuid,path LIMIT 100", after, path); err != nil {
			return err
		}
		if len(links) == 0 {
			return nil
		}
		for _, link := range links {
			child, err := findSourceCapture(conn.Get, conn.Select, link.CaptureUUID)
			if err != nil {
				return err
			}
			parent, err := findSourceCapture(conn.Get, conn.Select, link.ParentUUID)
			if err != nil {
				return err
			}
			if err := archive.ValidateCaptureContext(link, child, parent); err != nil {
				return fmt.Errorf("capture context %s%s: %w", link.CaptureUUID, link.Path, models.ErrSourcePayloadCorrupt)
			}
			after, path = link.CaptureUUID, link.Path
		}
	}
}
