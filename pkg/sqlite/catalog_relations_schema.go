package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateCatalogRelationsSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"catalog_relations_imports", "catalog_relation_records", "catalog_relations_import_guard", "catalog_relation_record_immutable", "catalog_relation_review"} {
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
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM catalog_relations_imports i
 LEFT JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid LEFT JOIN catalog_evidence_imports e ON e.snapshot_uuid=i.snapshot_uuid WHERE
 s.state IS NOT 'received' OR i.manifest_sha256 IS NOT s.manifest_sha256 OR e.state IS NULL OR e.state='running'
 OR i.manifest_sha256 IS NOT e.manifest_sha256
 OR i.source_records!=(SELECT coalesce(sum(t.received_records),0) FROM catalog_snapshot_tables t WHERE t.snapshot_uuid=s.uuid AND t.source_table IN `+catalogRelationTablesSQL+`)
 OR i.processed_records!=(SELECT count(*) FROM catalog_relation_records r WHERE r.snapshot_uuid=s.uuid)
 OR i.last_ordinal!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_relation_records r WHERE r.snapshot_uuid=s.uuid)
 OR i.processed_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=s.uuid AND r.ordinal<=i.last_ordinal AND r.source_table IN `+catalogRelationTablesSQL+`)
 OR i.mapped_records!=(SELECT count(*) FROM catalog_relation_records r WHERE r.snapshot_uuid=s.uuid AND r.outcome='mapped')
 OR i.review_records!=(SELECT count(*) FROM catalog_relation_records r WHERE r.snapshot_uuid=s.uuid AND r.outcome='review')
 OR i.unassigned_records!=(SELECT count(*) FROM catalog_relation_records r WHERE r.snapshot_uuid=s.uuid AND r.outcome='unassigned')
 OR (i.state='mapped' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0))
 OR EXISTS(SELECT 1 FROM catalog_relation_records r LEFT JOIN catalog_snapshot_records s ON s.snapshot_uuid=r.snapshot_uuid AND s.ordinal=r.ordinal WHERE
 r.source_table IS NOT s.source_table OR r.source_key IS NOT s.source_key OR r.data_sha256 IS NOT s.data_sha256
 OR r.source_values IS NOT json_extract(s.data,'$.values'))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has incomplete catalog relationship import receipts")
	}
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM catalog_relation_records r
 LEFT JOIN source_account_identifiers i ON i.uuid=r.identifier_uuid
 LEFT JOIN source_account_identifier_evidence e ON e.identifier_uuid=r.identifier_uuid AND e.evidence_key=r.identifier_evidence_key
 WHERE r.identifier_uuid IS NOT NULL AND (i.account_uuid IS NOT r.account_uuid OR e.origin IS NOT 'migration'
 OR json_extract(e.details,'$.snapshot_uuid') IS NOT r.snapshot_uuid OR json_extract(e.details,'$.source_sha256') IS NOT r.data_sha256))
 OR EXISTS(SELECT 1 FROM catalog_relation_records r LEFT JOIN source_post_url_evidence e ON e.uuid=r.url_evidence_uuid
 LEFT JOIN source_post_urls u ON u.uuid=e.url_uuid WHERE r.url_evidence_uuid IS NOT NULL AND (u.post_uuid IS NOT r.post_uuid OR e.origin IS NOT 'migration'
 OR json_extract(e.details,'$.snapshot_uuid') IS NOT r.snapshot_uuid OR json_extract(e.details,'$.source_sha256') IS NOT r.data_sha256))
 OR EXISTS(SELECT 1 FROM catalog_relation_records r LEFT JOIN source_post_identifier_evidence e ON e.uuid=r.post_identifier_evidence_uuid
 WHERE r.post_identifier_evidence_uuid IS NOT NULL AND (e.post_uuid IS NOT r.post_uuid OR e.origin IS NOT 'migration'
 OR json_extract(e.details,'$.snapshot_uuid') IS NOT r.snapshot_uuid OR json_extract(e.details,'$.source_sha256') IS NOT r.data_sha256))
 OR EXISTS(SELECT 1 FROM catalog_relation_records r LEFT JOIN source_post_account_claims e ON e.uuid=r.account_claim_uuid
 WHERE r.account_claim_uuid IS NOT NULL AND (e.post_uuid IS NOT r.post_uuid OR e.account_uuid IS NOT r.account_uuid OR e.origin IS NOT 'migration'
 OR json_extract(e.details,'$.snapshot_uuid') IS NOT r.snapshot_uuid OR json_extract(e.details,'$.source_sha256') IS NOT r.data_sha256))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has incomplete catalog relationship evidence mappings")
	}
	return nil
}
