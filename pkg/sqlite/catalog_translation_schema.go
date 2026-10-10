package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateCatalogTranslationSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"catalog_translation_imports", "catalog_translation_records", "catalog_translation_import_guard", "catalog_translation_record_immutable", "catalog_translation_review"} {
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
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM catalog_translation_imports i
 LEFT JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid LEFT JOIN catalog_evidence_imports e ON e.snapshot_uuid=i.snapshot_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=i.collection_uuid AND c.revision=i.collection_revision
 WHERE s.state IS NOT 'received' OR s.manifest_sha256 IS NOT i.manifest_sha256 OR s.collection_uuid IS NOT i.collection_uuid
 OR e.state IS NULL OR e.state='running' OR e.manifest_sha256 IS NOT i.manifest_sha256
 OR i.policy!='catalog-translations-v1' OR i.collection_revision!=1 OR c.origin IS NOT 'migration'
 OR i.source_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.source_table='translations')
 OR i.processed_records!=(SELECT count(*) FROM catalog_translation_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.mapped_records!=(SELECT count(*) FROM catalog_translation_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='mapped')
 OR i.review_records!=(SELECT count(*) FROM catalog_translation_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR i.last_ordinal!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_translation_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.processed_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.source_table='translations' AND r.ordinal<=i.last_ordinal)
 OR (i.state='mapped' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0))
 OR EXISTS(SELECT 1 FROM catalog_translation_records r
 LEFT JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN catalog_translation_imports i ON i.snapshot_uuid=r.snapshot_uuid
 LEFT JOIN catalog_snapshots snapshot ON snapshot.uuid=r.snapshot_uuid
 LEFT JOIN source_translations t ON t.uuid=r.translation_uuid
 LEFT JOIN source_translation_evidence x ON x.uuid=r.evidence_uuid
 WHERE e.source_table IS NOT 'translations'
 OR (r.translation_uuid IS NOT NULL AND (t.uuid IS NULL
  OR t.original_text IS NOT json_extract(e.data,'$.values.original_text') OR t.translated_text IS NOT json_extract(e.data,'$.values.translated_text')
  OR t.source_language IS NOT json_extract(e.data,'$.values.source_language') OR t.target_language IS NOT json_extract(e.data,'$.values.target_language')
  OR t.provider IS NOT json_extract(e.data,'$.values.provider')))
 OR (r.post_uuid IS NOT NULL AND r.post_uuid IS NOT (SELECT p.post_uuid FROM catalog_evidence_posts p
  WHERE p.snapshot_uuid=r.snapshot_uuid AND p.source_key=json_array(json_extract(e.data,'$.values.post_key'))))
 OR (r.evidence_uuid IS NOT NULL AND (x.uuid IS NULL OR x.post_uuid IS NOT r.post_uuid OR x.translation_uuid IS NOT r.translation_uuid
  OR x.collection_uuid IS NOT i.collection_uuid OR x.collection_revision IS NOT i.collection_revision OR x.origin IS NOT 'migration'
  OR x.provenance IS NOT json_extract(e.data,'$.values.provenance') OR x.captured_at IS NOT json_extract(e.data,'$.values.captured_at')
  OR x.declared_input_hash IS NOT json_extract(e.data,'$.values.input_hash')
  OR x.input_hash_algorithm IS NOT (CASE WHEN json_extract(e.data,'$.values.input_hash') IS NULL THEN '' ELSE 'catalog-json-sha256-v1' END)
  OR json_extract(x.details,'$.policy') IS NOT i.policy OR json_extract(x.details,'$.registry_source_uuid') IS NOT snapshot.source_uuid
  OR json_extract(x.details,'$.catalog_id') IS NOT snapshot.catalog_id
  OR json_extract(x.details,'$.legacy_translation_id') IS NOT json_extract(e.data,'$.values.translation_id')
  OR json_extract(x.details,'$.legacy_post_key') IS NOT json_extract(e.data,'$.values.post_key')))
 OR (r.outcome='mapped' AND r.input_hash_state NOT IN ('missing','verified','unverifiable')))`)
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid catalog translation progress or reference scope")
	}
	rows, err := conn.Query(`SELECT r.input_hash_state,json_extract(e.data,'$.values.original_text'),json_extract(e.data,'$.values.input_hash')
 FROM catalog_translation_records r JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 WHERE r.input_hash_state!=''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var original, declared *string
		if err := rows.Scan(&state, &original, &declared); err != nil {
			return err
		}
		expected, err := catalogTranslationHashState(original, declared)
		if err != nil || state != expected {
			return errors.New("native database has invalid catalog translation input identity")
		}
	}
	return rows.Err()
}
