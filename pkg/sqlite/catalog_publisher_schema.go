package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// Startup validates before planner statistics can be refreshed. Each receipt
// must look up its parent by the unique snapshot key even with stale statistics.
const catalogPublisherValidationQuery = `SELECT EXISTS(SELECT 1 FROM catalog_publisher_imports i
 LEFT JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid
 LEFT JOIN catalog_evidence_imports e ON e.snapshot_uuid=i.snapshot_uuid
 LEFT JOIN catalog_relations_imports rel ON rel.snapshot_uuid=i.snapshot_uuid WHERE
 s.state IS NOT 'received' OR i.manifest_sha256 IS NOT s.manifest_sha256
 OR e.state IS NULL OR e.state='running' OR i.manifest_sha256 IS NOT e.manifest_sha256
 OR rel.state IS NULL OR rel.state='running' OR i.manifest_sha256 IS NOT rel.manifest_sha256
 OR i.policy!='captured-account-v1'
 OR i.source_records IS NOT json_extract(s.manifest,'$.captures.count')
 OR i.source_records!=(SELECT count(*) FROM catalog_evidence_records WHERE snapshot_uuid=i.snapshot_uuid AND ` + catalogCaptureCandidates + `)
 OR i.processed_records!=(SELECT count(*) FROM catalog_publisher_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.last_ordinal!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_publisher_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.processed_records!=(SELECT count(*) FROM catalog_evidence_records WHERE snapshot_uuid=i.snapshot_uuid AND ordinal<=i.last_ordinal AND ` + catalogCaptureCandidates + `)
 OR i.linked_records!=(SELECT count(*) FROM catalog_publisher_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='linked')
 OR i.preserved_records!=(SELECT count(*) FROM catalog_publisher_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='preserved')
 OR i.review_records!=(SELECT count(*) FROM catalog_publisher_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR i.unavailable_records!=(SELECT count(*) FROM catalog_publisher_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='unavailable')
 OR i.created_accounts!=(SELECT count(*) FROM catalog_publisher_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.created_account=1)
 OR (i.state='mapped' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0))
 OR EXISTS(SELECT 1 FROM catalog_publisher_records r
 LEFT JOIN catalog_evidence_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN catalog_publisher_imports i INDEXED BY sqlite_autoindex_catalog_publisher_imports_1 ON i.snapshot_uuid=r.snapshot_uuid
 LEFT JOIN capture_publisher_decisions d ON d.uuid=r.decision_uuid WHERE
 e.source_table IS NULL OR e.source_table NOT IN ('observations','observation_details') OR e.outcome='shared'
 OR r.capture_uuid IS NOT e.capture_uuid
 OR json_extract(r.context_json,'$.policy') IS NOT i.policy
 OR (r.decision_uuid IS NOT NULL AND d.capture_uuid IS NOT r.capture_uuid)
 OR (r.outcome='linked' AND (d.state IS NOT 'linked' OR d.origin IS NOT 'capture'
  OR d.policy IS NOT i.policy OR json_extract(r.context_json,'$.action') NOT IN ('link','create')
  OR r.created_account IS NOT (json_extract(r.context_json,'$.action')='create')))
 OR (r.outcome='preserved' AND (d.state NOT IN ('linked','unlinked') OR d.state IS NULL
  OR json_extract(r.context_json,'$.current_decision_uuid') IS NOT r.decision_uuid)))`

func validateCatalogPublisherSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"catalog_publisher_imports", "catalog_publisher_records", "catalog_evidence_publisher_candidates", "catalog_publisher_import_guard", "catalog_publisher_record_immutable", "catalog_publisher_review"} {
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
	if err := conn.Get(&invalid, catalogPublisherValidationQuery); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has incomplete catalog publisher import receipts or decisions")
	}
	return nil
}
