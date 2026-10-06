package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validatePostMediaDecisionSchema(conn *sqlx.DB) error {
	for _, name := range []string{"post_media_decisions", "post_media_links", "post_media_decisions_media", "post_media_links_media",
		"post_media_decision_scope", "post_media_decision_immutable", "post_media_head_forward",
		"post_media_supersessions", "post_media_supersessions_decision", "post_media_supersession_scope", "post_media_supersession_immutable",
		"metadata_decision_post_media", "metadata_decision_post_media_source", "metadata_decision_post_media_scope", "metadata_decision_post_media_immutable"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var typed bool
	if err := conn.Get(&typed, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('post_media_decisions') WHERE name='created_at' AND upper(type)='DATETIME')`); err != nil {
		return err
	}
	if !typed {
		return errors.New("native database schema is incomplete: invalid post media timestamp")
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT
EXISTS(SELECT 1 FROM post_media_decisions d LEFT JOIN source_posts p ON p.uuid=d.post_uuid
LEFT JOIN archive_entities m ON m.uuid=d.media_uuid WHERE p.uuid IS NULL OR m.kind IS NULL OR m.kind NOT IN ('scene','image') OR d.post_revision>p.revision)
OR EXISTS(SELECT 1 FROM post_media_links l LEFT JOIN post_media_decisions d ON d.uuid=l.decision_uuid
WHERE d.post_uuid IS NOT l.post_uuid OR d.media_uuid IS NOT l.media_uuid)
OR EXISTS(SELECT 1 FROM post_media_supersessions s LEFT JOIN post_media_decisions old ON old.uuid=s.previous_uuid
LEFT JOIN post_media_decisions current ON current.uuid=s.decision_uuid
WHERE old.uuid IS NULL OR current.uuid IS NULL OR old.post_uuid!=current.post_uuid OR old.post_revision>=current.post_revision)
OR EXISTS(SELECT 1 FROM post_media_decisions d
LEFT JOIN post_media_links l ON l.decision_uuid=d.uuid LEFT JOIN post_media_supersessions s ON s.previous_uuid=d.uuid
WHERE (l.decision_uuid IS NULL)=(s.previous_uuid IS NULL))
OR EXISTS(SELECT 1 FROM metadata_decision_post_media l LEFT JOIN metadata_field_decisions d ON d.uuid=l.decision_uuid
LEFT JOIN source_captures c ON c.uuid=d.capture_uuid LEFT JOIN post_media_decisions p ON p.uuid=l.post_media_decision_uuid
WHERE d.uuid IS NULL OR d.origin!='source' OR c.uuid IS NULL OR p.uuid IS NULL OR p.post_uuid!=c.post_uuid OR p.state!='linked')`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid post media decisions or metadata provenance")
	}
	return nil
}
