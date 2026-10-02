package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateCatalogEvidenceSchema(conn *sqlx.DB) error {
	for _, name := range []string{"catalog_evidence_imports", "catalog_evidence_posts", "catalog_evidence_records", "catalog_evidence_import_guard", "catalog_evidence_post_immutable", "catalog_evidence_record_immutable", "catalog_evidence_review", "catalog_snapshot_observation_children", "catalog_snapshot_post_urls"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM catalog_evidence_imports i JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid WHERE
 s.state!='received' OR i.manifest_sha256!=s.manifest_sha256
 OR i.source_records!=(SELECT coalesce(sum(t.received_records),0) FROM catalog_snapshot_tables t WHERE t.snapshot_uuid=s.uuid AND t.source_table IN ('account_snapshots','posts','observations','observation_details'))
 OR i.processed_records!=(SELECT count(*) FROM catalog_evidence_records r WHERE r.snapshot_uuid=s.uuid)
 OR i.last_ordinal!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_evidence_records r WHERE r.snapshot_uuid=s.uuid)
 OR i.processed_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=s.uuid AND r.ordinal<=i.last_ordinal AND r.source_table IN ('account_snapshots','posts','observations','observation_details'))
 OR i.review_records!=(SELECT count(*) FROM catalog_evidence_records r WHERE r.snapshot_uuid=s.uuid AND r.outcome='review')
 OR i.capture_mappings!=(SELECT count(*) FROM catalog_evidence_records r WHERE r.snapshot_uuid=s.uuid AND r.capture_uuid IS NOT NULL)
 OR i.profile_mappings!=(SELECT count(*) FROM catalog_evidence_records r WHERE r.snapshot_uuid=s.uuid AND r.profile_hash IS NOT NULL)
 OR (i.state='mapped' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0))
 OR EXISTS(SELECT 1 FROM catalog_evidence_records r LEFT JOIN catalog_snapshot_records s ON s.snapshot_uuid=r.snapshot_uuid AND s.ordinal=r.ordinal WHERE
 r.source_table IS NOT s.source_table OR r.source_key IS NOT s.source_key OR r.data_sha256 IS NOT s.data_sha256)
 OR EXISTS(SELECT 1 FROM catalog_evidence_posts p LEFT JOIN catalog_snapshot_records s ON s.snapshot_uuid=p.snapshot_uuid AND s.ordinal=p.source_ordinal WHERE
 s.source_table IS NOT 'posts' OR p.source_key IS NOT s.source_key OR p.data_sha256 IS NOT s.data_sha256)
 OR EXISTS(SELECT 1 FROM catalog_evidence_records r JOIN source_captures c ON c.uuid=r.capture_uuid JOIN catalog_snapshots s ON s.uuid=r.snapshot_uuid WHERE
 c.post_uuid IS NOT r.post_uuid OR r.header_sha256 IS NULL
 OR NOT EXISTS(SELECT 1 FROM source_collection_captures cc WHERE cc.capture_uuid=c.uuid AND cc.collection_uuid=s.collection_uuid AND cc.collection_revision=1))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has incomplete catalog evidence import receipts or mappings")
	}
	return nil
}
