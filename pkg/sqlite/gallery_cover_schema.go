package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateGalleryCoverSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"gallery_covers", "gallery_covers_media", "gallery_cover_scope_insert", "gallery_cover_scope_update",
		"gallery_cover_image_removed", "gallery_cover_scene_removed", "gallery_cover_media_retired",
		"gallery_cover_image_replaced", "gallery_cover_scene_replaced",
		"gallery_cover_changed_insert", "gallery_cover_changed_update", "gallery_cover_changed_delete"} {
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
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM gallery_covers c
LEFT JOIN galleries g ON g.id=c.gallery_id LEFT JOIN archive_entities a ON a.uuid=c.media_uuid
WHERE g.id IS NULL OR a.uuid IS NULL OR a.state!='active' OR NOT (
 (a.kind='image' AND EXISTS(SELECT 1 FROM galleries_images gi WHERE gi.gallery_id=c.gallery_id AND gi.image_id=a.image_id)) OR
 (a.kind='scene' AND EXISTS(SELECT 1 FROM scenes_galleries gs WHERE gs.gallery_id=c.gallery_id AND gs.scene_id=a.scene_id))))`); err != nil {
		return err
	}
	if invalid {
		return fmt.Errorf("gallery cover does not identify an active gallery member")
	}
	return nil
}
