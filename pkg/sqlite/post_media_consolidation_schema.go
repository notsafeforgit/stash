package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validatePostMediaConsolidationSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"post_media_supersessions_scope", "post_media_consolidation_edges", "post_media_consolidation_edges_receipt",
		"post_media_consolidation_edge_scope", "post_media_consolidation_edge_immutable"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native post media consolidation schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM post_media_consolidation_edges e
LEFT JOIN post_media_decisions old ON old.uuid=e.previous_uuid
LEFT JOIN post_media_decisions current ON current.uuid=e.decision_uuid
LEFT JOIN post_media_supersessions replacement ON replacement.previous_uuid=e.previous_uuid AND replacement.decision_uuid=e.decision_uuid
LEFT JOIN source_post_consolidations c ON c.uuid=e.consolidation_uuid
WHERE old.uuid IS NULL OR current.uuid IS NULL OR replacement.previous_uuid IS NULL OR c.uuid IS NULL
OR old.post_uuid=current.post_uuid OR c.destination_uuid!=current.post_uuid OR current.post_revision<=c.destination_revision
OR current.post_uuid NOT IN (WITH RECURSIVE ancestors(uuid,depth) AS (
 SELECT old.post_uuid,0 UNION ALL SELECT h.destination_uuid,a.depth+1
 FROM ancestors a JOIN source_post_consolidations h ON h.source_uuid=a.uuid
 WHERE h.sequence<=c.sequence AND a.depth<256
) SELECT uuid FROM ancestors)
OR current.media_uuid NOT IN (WITH RECURSIVE targets(uuid,depth) AS (
 SELECT old.media_uuid,0 UNION ALL SELECT a.redirect_to,t.depth+1 FROM targets t JOIN archive_entities a ON a.uuid=t.uuid
 WHERE a.state='redirected' AND t.depth<127
) SELECT uuid FROM targets))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid post media consolidation evidence")
	}
	return nil
}
