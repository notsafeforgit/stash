package sqlite

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/jmoiron/sqlx"
)

// Startup validates before planner statistics can be refreshed. Each receipt
// must look up its parent by the unique snapshot key even with stale statistics.
const catalogMediaValidationQuery = `SELECT EXISTS(SELECT 1 FROM catalog_media_imports i
 LEFT JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid
 LEFT JOIN catalog_evidence_imports e ON e.snapshot_uuid=i.snapshot_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=s.collection_uuid AND c.revision=i.collection_revision
 LEFT JOIN media_root_revisions root ON root.root_uuid=i.root_uuid AND root.revision=i.root_revision
 WHERE s.state IS NOT 'received' OR i.manifest_sha256 IS NOT s.manifest_sha256
 OR e.state IS NULL OR e.state='running' OR e.manifest_sha256 IS NOT i.manifest_sha256
 OR c.collection_uuid IS NULL OR root.root_uuid IS NULL OR i.policy!='catalog-media-v1'
 OR i.source_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.source_table IN ('assets','files','appearances'))
 OR i.processed_records!=(SELECT count(*) FROM catalog_media_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.mapped_records!=(SELECT count(*) FROM catalog_media_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='mapped')
 OR i.review_records!=(SELECT count(*) FROM catalog_media_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR i.unavailable_records!=(SELECT count(*) FROM catalog_media_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='unavailable')
 OR i.matched_files!=(SELECT count(*) FROM catalog_media_records r JOIN catalog_snapshot_records s ON s.snapshot_uuid=r.snapshot_uuid AND s.ordinal=r.ordinal
   WHERE r.snapshot_uuid=i.snapshot_uuid AND s.source_table='files' AND r.match_uuid IS NOT NULL)
 OR i.media_associations!=(SELECT count(*) FROM catalog_media_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.media_evidence_uuid IS NOT NULL)
 OR (i.state='mapped' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0)
 OR (i.phase='complete' AND i.last_ordinal!=0)
 OR (i.phase!='complete' AND i.last_ordinal!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_media_records r
  JOIN catalog_snapshot_records s ON s.snapshot_uuid=r.snapshot_uuid AND s.ordinal=r.ordinal WHERE r.snapshot_uuid=i.snapshot_uuid AND s.source_table=i.phase))
 OR (i.phase!='complete' AND (SELECT count(*) FROM catalog_media_records r JOIN catalog_snapshot_records s ON s.snapshot_uuid=r.snapshot_uuid AND s.ordinal=r.ordinal
  WHERE r.snapshot_uuid=i.snapshot_uuid AND s.source_table=i.phase)!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.source_table=i.phase AND r.ordinal<=i.last_ordinal)))
 OR EXISTS(SELECT 1 FROM catalog_media_records r
 LEFT JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN catalog_snapshots s ON s.uuid=r.snapshot_uuid LEFT JOIN catalog_media_imports i INDEXED BY sqlite_autoindex_catalog_media_imports_1 ON i.snapshot_uuid=r.snapshot_uuid
 LEFT JOIN source_content_claims c ON c.uuid=r.claim_uuid
 LEFT JOIN source_file_observations o ON o.uuid=r.observation_uuid
 LEFT JOIN source_file_matches m ON m.uuid=r.match_uuid
 LEFT JOIN source_post_file_evidence p ON p.uuid=r.post_file_uuid
 LEFT JOIN source_media_evidence media ON media.uuid=r.media_evidence_uuid
 WHERE e.source_table IS NULL OR e.source_table NOT IN ('assets','files','appearances') OR json_extract(r.context_json,'$.policy') IS NOT i.policy
 OR (r.claim_uuid IS NOT NULL AND (c.collection_uuid IS NOT s.collection_uuid OR c.collection_revision IS NOT i.collection_revision
  OR c.reference_namespace IS NOT ('legacy:catalog:'||s.source_uuid||':'||s.catalog_id)
  OR c.reference_value IS NOT json_extract(e.data,'$.values.asset_id')))
 OR (r.observation_uuid IS NOT NULL AND (o.collection_uuid IS NOT s.collection_uuid OR o.collection_revision IS NOT i.collection_revision
  OR o.root_uuid IS NOT i.root_uuid OR o.root_revision IS NOT i.root_revision OR o.content_claim_uuid IS NOT r.claim_uuid))
 OR (r.match_uuid IS NOT NULL AND m.observation_uuid IS NOT r.observation_uuid)
 OR (r.post_file_uuid IS NOT NULL AND (p.post_uuid IS NOT r.post_uuid OR p.observation_uuid IS NOT r.observation_uuid))
 OR (r.media_evidence_uuid IS NOT NULL AND (media.post_uuid IS NOT r.post_uuid OR media.file_uuid IS NOT m.file_uuid OR media.basis IS NOT 'legacy'
  OR json_extract(media.details,'$.source_post_file_evidence_uuid') IS NOT r.post_file_uuid OR json_extract(media.details,'$.source_file_match_uuid') IS NOT r.match_uuid))
 OR (e.source_table='assets' AND (r.observation_uuid IS NOT NULL OR r.post_uuid IS NOT NULL OR (r.outcome='mapped' AND r.claim_uuid IS NULL)))
 OR (e.source_table='files' AND (r.post_uuid IS NOT NULL OR (r.outcome='mapped' AND r.match_uuid IS NULL)))
 OR (e.source_table='appearances' AND r.outcome='mapped' AND r.media_evidence_uuid IS NULL))`

func validateCatalogMediaSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"catalog_media_imports", "catalog_media_records", "catalog_media_import_guard", "catalog_media_record_immutable", "catalog_media_review", "catalog_media_claim", "catalog_snapshot_record_phase"} {
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
	err := conn.Get(&invalid, catalogMediaValidationQuery)
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid catalog media import bindings or receipts")
	}
	for _, phase := range []struct{ name, earlier, later string }{
		{"assets", "('files','appearances','complete')", "('')"},
		{"files", "('appearances','complete')", "('assets')"},
		{"appearances", "('complete')", "('assets','files')"},
	} {
		err = conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM catalog_media_imports i WHERE
 (i.phase IN `+phase.earlier+` AND (SELECT count(*) FROM catalog_media_records r JOIN catalog_snapshot_records s ON s.snapshot_uuid=r.snapshot_uuid AND s.ordinal=r.ordinal
  WHERE r.snapshot_uuid=i.snapshot_uuid AND s.source_table=?)!=(SELECT count(*) FROM catalog_snapshot_records s WHERE s.snapshot_uuid=i.snapshot_uuid AND s.source_table=?))
 OR (i.phase IN `+phase.later+` AND EXISTS(SELECT 1 FROM catalog_media_records r JOIN catalog_snapshot_records s ON s.snapshot_uuid=r.snapshot_uuid AND s.ordinal=r.ordinal
  WHERE r.snapshot_uuid=i.snapshot_uuid AND s.source_table=?)))`, phase.name, phase.name, phase.name)
		if err != nil {
			return err
		}
		if invalid {
			return errors.New("native database has incomplete catalog media phase checkpoints")
		}
	}
	rows, err := conn.Query("SELECT library_root_path FROM catalog_media_imports")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return err
		}
		if !validAccountText(path, 4096, false) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("native database has invalid catalog media mount mapping")
		}
	}
	return rows.Err()
}
