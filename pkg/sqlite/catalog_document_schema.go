package sqlite

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Startup validates before planner statistics can be refreshed. Each receipt
// must look up its parent by the unique snapshot key even with stale statistics.
const catalogDocumentValidationQuery = `SELECT EXISTS(SELECT 1 FROM catalog_document_imports i
 LEFT JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid
 LEFT JOIN catalog_evidence_imports e ON e.snapshot_uuid=i.snapshot_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=i.collection_uuid AND c.revision=i.collection_revision
 WHERE s.state IS NOT 'received' OR s.manifest_sha256 IS NOT i.manifest_sha256 OR s.collection_uuid IS NOT i.collection_uuid
 OR e.state IS NULL OR e.state='running' OR e.manifest_sha256 IS NOT i.manifest_sha256
 OR i.policy!='catalog-documents-v1' OR i.collection_revision!=1 OR c.origin IS NOT 'migration'
 OR i.source_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.source_table IN ('sidecar_documents','sidecar_sources','sidecars','sidecar_heads'))
 OR i.processed_records!=(SELECT count(*) FROM catalog_document_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.mapped_records!=(SELECT count(*) FROM catalog_document_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='mapped')
 OR i.review_records!=(SELECT count(*) FROM catalog_document_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR (i.state='mapped' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0)
 OR (i.phase='complete' AND i.last_ordinal!=0)
 OR (i.phase!='complete' AND i.last_ordinal!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_document_records r
  JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal WHERE r.snapshot_uuid=i.snapshot_uuid AND e.source_table=i.phase))
 OR (i.phase!='complete' AND (SELECT count(*) FROM catalog_document_records r JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
  WHERE r.snapshot_uuid=i.snapshot_uuid AND e.source_table=i.phase)!=(SELECT count(*) FROM catalog_snapshot_records e WHERE e.snapshot_uuid=i.snapshot_uuid AND e.source_table=i.phase AND e.ordinal<=i.last_ordinal)))
 OR EXISTS(SELECT 1 FROM catalog_document_records r
 LEFT JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN catalog_document_imports i INDEXED BY sqlite_autoindex_catalog_document_imports_1 ON i.snapshot_uuid=r.snapshot_uuid
 LEFT JOIN catalog_snapshots snapshot ON snapshot.uuid=r.snapshot_uuid
 LEFT JOIN source_documents d ON d.uuid=r.document_uuid
 LEFT JOIN source_document_sources s ON s.uuid=r.source_uuid
 LEFT JOIN source_document_head_claims c ON c.uuid=r.claim_uuid
 LEFT JOIN source_document_head_decisions h ON h.uuid=r.head_uuid
 WHERE e.source_table IS NULL OR e.source_table NOT IN ('sidecar_documents','sidecar_sources','sidecars','sidecar_heads')
 OR (r.document_uuid IS NOT NULL AND d.uuid IS NULL)
 OR (r.document_uuid IS NOT NULL AND e.source_table IN ('sidecar_documents','sidecars') AND
  (d.content_sha256 IS NOT json_extract(e.data,'$.values.content_sha256') OR d.encoding IS NOT json_extract(e.data,'$.values.encoding')
  OR d.parser IS NOT 'legacy-catalog-nfo-v1' OR d.parse_status IS NOT json_extract(e.data,'$.values.parse_status')
  OR d.warnings IS NOT json_extract(e.data,'$.values.warnings_json') OR d.parsed IS NOT json_extract(e.data,'$.values.parsed_json')))
 OR (e.source_table='sidecar_sources' AND r.document_uuid IS NOT NULL AND r.document_uuid IS NOT (
  SELECT parent.document_uuid FROM catalog_snapshot_records original JOIN catalog_document_records parent ON parent.snapshot_uuid=original.snapshot_uuid AND parent.ordinal=original.ordinal
  WHERE original.snapshot_uuid=r.snapshot_uuid AND original.source_table='sidecar_documents' AND original.source_key=json_array(json_extract(e.data,'$.values.document_id'))))
 OR (r.source_uuid IS NOT NULL AND (s.document_uuid IS NOT r.document_uuid OR s.post_uuid IS NOT r.post_uuid
  OR s.collection_uuid IS NOT i.collection_uuid OR s.collection_revision IS NOT i.collection_revision OR s.origin IS NOT 'migration'
  OR s.relative_path IS NOT json_extract(e.data,'$.values.relpath') OR d.content_sha256 IS NOT json_extract(e.data,'$.values.content_sha256')
  OR json_extract(s.details,'$.policy') IS NOT i.policy OR json_extract(s.details,'$.basis') IS NOT 'source'
  OR json_extract(s.details,'$.registry_source_uuid') IS NOT snapshot.source_uuid OR json_extract(s.details,'$.catalog_id') IS NOT snapshot.catalog_id))
 OR (r.source_uuid IS NOT NULL AND e.source_table IN ('sidecar_sources','sidecars') AND
  (s.captured_at IS NOT json_extract(e.data,'$.values.captured_at') OR json_extract(s.details,'$.legacy_post_key') IS NOT json_extract(e.data,'$.values.post_key')
  OR (r.post_uuid IS NOT NULL AND r.post_uuid IS NOT (SELECT p.post_uuid FROM catalog_evidence_posts p WHERE p.snapshot_uuid=r.snapshot_uuid AND p.source_key=json_array(json_extract(e.data,'$.values.post_key'))))
  OR ((r.post_uuid IS NULL)!=(json_extract(e.data,'$.values.post_key') IS NULL))))
 OR (e.source_table='sidecar_heads' AND r.source_uuid IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM catalog_snapshot_records original JOIN catalog_document_records parent ON parent.snapshot_uuid=original.snapshot_uuid AND parent.ordinal=original.ordinal
  WHERE original.snapshot_uuid=r.snapshot_uuid AND original.source_table IN ('sidecar_sources','sidecars')
  AND original.source_key=json_array(json_extract(e.data,'$.values.relpath'),json_extract(e.data,'$.values.content_sha256')) AND parent.source_uuid=r.source_uuid))
 OR (r.claim_uuid IS NOT NULL AND (c.source_uuid IS NOT r.source_uuid OR c.collection_uuid IS NOT i.collection_uuid
  OR c.relative_path IS NOT s.relative_path OR c.origin IS NOT 'migration'
  OR c.observed_at IS NOT (CASE r.selection_basis WHEN 'explicit' THEN json_extract(e.data,'$.values.observed_at') ELSE s.captured_at END)
  OR json_extract(c.details,'$.policy') IS NOT i.policy OR json_extract(c.details,'$.basis') IS NOT r.selection_basis
  OR json_extract(c.details,'$.registry_source_uuid') IS NOT snapshot.source_uuid OR json_extract(c.details,'$.catalog_id') IS NOT snapshot.catalog_id))
 OR (r.head_uuid IS NOT NULL AND (h.collection_uuid IS NOT i.collection_uuid OR h.relative_path IS NOT s.relative_path
  OR (r.outcome='mapped' AND (h.state IS NOT 'linked' OR h.source_uuid IS NOT r.source_uuid))))
 OR (e.source_table='sidecar_documents' AND (r.source_uuid IS NOT NULL OR r.post_uuid IS NOT NULL OR r.selection_basis!=''))
 OR (e.source_table='sidecar_heads' AND r.selection_basis!='explicit')
 OR (e.source_table IN ('sidecar_sources','sidecars') AND r.selection_basis='explicit')
 OR (e.source_table!='sidecar_documents' AND r.outcome='mapped' AND r.source_uuid IS NULL)
 OR (r.selection_basis='legacy_fallback' AND (EXISTS(SELECT 1 FROM catalog_snapshot_records explicit
  WHERE explicit.snapshot_uuid=r.snapshot_uuid AND explicit.source_table='sidecar_heads' AND explicit.source_key=json_array(s.relative_path))
  OR r.ordinal IS NOT (SELECT candidate.ordinal FROM catalog_snapshot_records candidate
   WHERE candidate.snapshot_uuid=r.snapshot_uuid AND candidate.source_table=e.source_table AND candidate.source_table IN ('sidecars','sidecar_sources')
   AND json_extract(candidate.data,'$.values.relpath')=s.relative_path
   ORDER BY json_extract(candidate.data,'$.values.captured_at') DESC,json_extract(candidate.data,'$.values.content_sha256') DESC LIMIT 1))))`

func validateCatalogDocumentSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"catalog_document_imports", "catalog_document_records", "catalog_document_import_guard", "catalog_document_record_immutable", "catalog_document_review", "catalog_snapshot_documents_path"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	imports := "catalog_document_imports"
	var compactable bool
	if err := conn.Get(&compactable, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='catalog_document_pending_imports')`); err != nil {
		return err
	}
	if !auditData {
		return nil
	}
	if compactable {
		imports = "catalog_document_pending_imports"
		if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM catalog_snapshots s
 WHERE s.nfo_compacted=1 AND (NOT EXISTS(SELECT 1 FROM catalog_document_imports i WHERE i.snapshot_uuid=s.uuid AND i.state='mapped' AND i.phase='complete')
 OR EXISTS(SELECT 1 FROM catalog_document_records r WHERE r.snapshot_uuid=s.uuid)))`); err != nil {
			return err
		}
		if invalid {
			return errors.New("compacted NFO import still has document records or unfinished work")
		}
	}
	// The remaining document rows keep their ordinary checks. Only the aggregate
	// progress check skips imports whose original NFO inputs were discarded.
	query := strings.Replace(catalogDocumentValidationQuery, "FROM catalog_document_imports i", "FROM "+imports+" i", 1)
	err := conn.Get(&invalid, query)
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid catalog document progress or reference scope")
	}
	for n, phase := range catalogDocumentPhases[:4] {
		later := "('" + strings.Join(catalogDocumentPhases[n+1:], "','") + "')"
		earlier := "('" + strings.Join(catalogDocumentPhases[:n], "','") + "')"
		err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM `+imports+` i WHERE
 (i.phase IN `+later+` AND (SELECT count(*) FROM catalog_document_records r JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
  WHERE r.snapshot_uuid=i.snapshot_uuid AND e.source_table=?)!=(SELECT count(*) FROM catalog_snapshot_records e WHERE e.snapshot_uuid=i.snapshot_uuid AND e.source_table=?))
 OR (i.phase IN `+earlier+` AND EXISTS(SELECT 1 FROM catalog_document_records r JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
  WHERE r.snapshot_uuid=i.snapshot_uuid AND e.source_table=?)))`, phase, phase, phase)
		if err != nil {
			return err
		}
		if invalid {
			return errors.New("native database has incomplete catalog document phase checkpoints")
		}
	}
	return nil
}
