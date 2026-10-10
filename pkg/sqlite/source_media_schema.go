package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validatePostMediaEvidenceSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"source_media_evidence_post", "source_media_evidence_active_post", "source_media_evidence_revision"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var typed bool
	if err := conn.Get(&typed, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('source_media_evidence')
 WHERE name='post_uuid' AND type='TEXT' AND "notnull"=1)
 AND EXISTS(SELECT 1 FROM pragma_table_info('source_media_evidence') WHERE name='created_at' AND upper(type)='DATETIME')
 AND (SELECT count(*) FROM pragma_table_info('source_media_evidence')
 WHERE name IN ('attachment_uuid','capture_uuid','manifest_uuid','position') AND "notnull"=0)=4`); err != nil {
		return err
	}
	if !typed {
		return errors.New("native database schema is incomplete: invalid source_media_evidence scope columns")
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_media_evidence e
 LEFT JOIN source_posts p ON p.uuid=e.post_uuid
 LEFT JOIN source_attachments a ON a.uuid=e.attachment_uuid
 LEFT JOIN source_captures c ON c.uuid=e.capture_uuid
 LEFT JOIN source_capture_attachment_manifests cm ON cm.capture_uuid=e.capture_uuid AND cm.manifest_uuid=e.manifest_uuid
 LEFT JOIN source_attachment_entries ae ON ae.manifest_uuid=e.manifest_uuid AND ae.position=e.position AND ae.attachment_uuid=e.attachment_uuid
 LEFT JOIN archive_entities m ON m.uuid=e.media_uuid
 LEFT JOIN archive_entities f ON f.uuid=e.file_uuid
 WHERE p.uuid IS NULL OR m.kind IS NULL OR m.kind NOT IN ('scene','image')
 OR (e.file_uuid IS NOT NULL AND (f.kind IS NULL OR f.kind!='file'))
 OR (e.attachment_uuid IS NOT NULL AND a.post_uuid IS NOT e.post_uuid)
 OR (e.capture_uuid IS NOT NULL AND c.post_uuid IS NOT e.post_uuid)
 OR (e.manifest_uuid IS NULL)!=(e.position IS NULL)
 OR (e.manifest_uuid IS NOT NULL)!=(e.attachment_uuid IS NOT NULL AND e.capture_uuid IS NOT NULL)
 OR (e.manifest_uuid IS NOT NULL AND (cm.post_uuid IS NOT e.post_uuid OR ae.post_uuid IS NOT e.post_uuid))
 OR (e.basis IN ('observed-file','verified-bytes') AND (e.attachment_uuid IS NULL OR e.capture_uuid IS NULL OR e.file_uuid IS NULL)))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid source media evidence scope or targets")
	}
	return nil
}
