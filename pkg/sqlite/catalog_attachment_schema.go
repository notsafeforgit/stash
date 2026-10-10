package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// Startup validates before planner statistics can be refreshed. Each receipt
// must look up its parent by the unique snapshot key even with stale statistics.
const catalogAttachmentValidationQuery = `SELECT EXISTS(SELECT 1 FROM catalog_attachment_imports i
 LEFT JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid
 LEFT JOIN catalog_evidence_imports e ON e.snapshot_uuid=i.snapshot_uuid WHERE
 s.state IS NOT 'received' OR i.manifest_sha256 IS NOT s.manifest_sha256
 OR e.state IS NULL OR e.state='running' OR i.manifest_sha256 IS NOT e.manifest_sha256
 OR i.policy!='captured-attachments-v2'
 OR i.source_records IS NOT json_extract(s.manifest,'$.captures.count')
 OR i.source_records!=(SELECT count(*) FROM catalog_evidence_records WHERE snapshot_uuid=i.snapshot_uuid AND ` + catalogCaptureCandidates + `)
 OR i.processed_records!=(SELECT count(*) FROM catalog_attachment_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.last_ordinal!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_attachment_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.processed_records!=(SELECT count(*) FROM catalog_evidence_records WHERE snapshot_uuid=i.snapshot_uuid AND ordinal<=i.last_ordinal AND ` + catalogCaptureCandidates + `)
 OR i.mapped_records!=(SELECT count(*) FROM catalog_attachment_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='mapped')
 OR i.preserved_records!=(SELECT count(*) FROM catalog_attachment_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='preserved')
 OR i.review_records!=(SELECT count(*) FROM catalog_attachment_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR i.unavailable_records!=(SELECT count(*) FROM catalog_attachment_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='unavailable')
 OR i.changed_selections!=(SELECT count(*) FROM catalog_attachment_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.selection_changed=1)
 OR (i.state='mapped' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0))
 OR EXISTS(SELECT 1 FROM catalog_attachment_records r
 LEFT JOIN catalog_evidence_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN catalog_attachment_imports i INDEXED BY sqlite_autoindex_catalog_attachment_imports_1 ON i.snapshot_uuid=r.snapshot_uuid
 LEFT JOIN post_attachment_decisions d ON d.uuid=r.selection_uuid WHERE
 e.source_table IS NULL OR e.source_table NOT IN ('observations','observation_details') OR e.outcome='shared'
 OR r.capture_uuid IS NOT e.capture_uuid OR r.post_uuid IS NOT e.post_uuid
 OR json_extract(r.context_json,'$.policy') IS NOT i.policy
 OR (r.selection_uuid IS NOT NULL AND d.post_uuid IS NOT r.post_uuid)
 OR (r.outcome='mapped' AND (d.mode IS NOT 'automatic'
  OR json_extract(r.context_json,'$.action') NOT IN ('selected','unchanged')
  OR r.selection_changed IS NOT (json_extract(r.context_json,'$.action')='selected')))
 OR (r.selection_changed=1 AND (d.origin IS NOT 'migration' OR d.capture_uuid IS NOT r.capture_uuid))
 OR (r.outcome='preserved' AND (d.mode IS NULL OR d.mode NOT IN ('pinned','disabled')
  OR json_extract(r.context_json,'$.action') IS NOT 'protected'
  OR json_extract(r.context_json,'$.current_selection_uuid') IS NOT r.selection_uuid))
 OR (r.outcome='mapped' AND r.selection_changed=0
  AND json_extract(r.context_json,'$.current_selection_uuid') IS NOT r.selection_uuid))`

func validateCatalogAttachmentSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"catalog_attachment_imports", "catalog_attachment_records", "catalog_attachment_import_guard", "catalog_attachment_record_immutable", "catalog_attachment_review"} {
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
	if err := conn.Get(&invalid, catalogAttachmentValidationQuery); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has incomplete catalog attachment import receipts or selections")
	}
	return nil
}
