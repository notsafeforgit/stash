package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateMediaConversionSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"media_conversions", "media_conversion_scope", "media_conversion_immutable", "media_conversion_retained"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM media_conversions c
 LEFT JOIN archive_entities i ON i.uuid=c.image_uuid LEFT JOIN archive_entities s ON s.uuid=c.scene_uuid
 LEFT JOIN archive_entities old ON old.uuid=c.original_file_uuid LEFT JOIN archive_entities new ON new.uuid=c.file_uuid
 LEFT JOIN file_content_versions v ON v.file_uuid=c.file_uuid AND v.generation=c.generation
 WHERE i.uuid IS NULL OR i.kind!='image' OR i.state!='redirected' OR i.redirect_to!=c.scene_uuid
 OR s.uuid IS NULL OR s.kind!='scene' OR old.uuid IS NULL OR old.kind!='file' OR old.state!='deleted'
 OR new.uuid IS NULL OR new.kind!='file' OR v.file_uuid IS NULL)`); err != nil {
		return err
	}
	if invalid {
		return fmt.Errorf("incomplete image to scene conversion")
	}
	return nil
}
