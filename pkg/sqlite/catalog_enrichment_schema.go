package sqlite

import (
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func validateCatalogEnrichmentSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"source_enrichment_receipts", "source_enrichment_receipts_post", "source_enrichment_receipts_collection",
		"source_enrichment_receipt_immutable", "source_enrichment_receipt_scope", "catalog_enrichment_imports", "catalog_enrichment_records",
		"catalog_enrichment_import_guard", "catalog_enrichment_record_immutable", "catalog_enrichment_review", "catalog_enrichment_receipt_sources"} {
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
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM catalog_enrichment_imports i
 LEFT JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid LEFT JOIN catalog_evidence_imports e ON e.snapshot_uuid=i.snapshot_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=i.collection_uuid AND c.revision=i.collection_revision
 WHERE s.state IS NOT 'received' OR s.manifest_sha256 IS NOT i.manifest_sha256 OR s.collection_uuid IS NOT i.collection_uuid
 OR e.state IS NULL OR e.state='running' OR e.manifest_sha256 IS NOT i.manifest_sha256
 OR i.policy!='catalog-enrichment-v1' OR i.collection_revision!=1 OR c.origin IS NOT 'migration'
 OR i.source_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.source_table='enrichment_receipts')
 OR i.processed_records!=(SELECT count(*) FROM catalog_enrichment_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.mapped_records!=(SELECT count(*) FROM catalog_enrichment_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='mapped')
 OR i.review_records!=(SELECT count(*) FROM catalog_enrichment_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR i.last_ordinal!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_enrichment_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.processed_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.source_table='enrichment_receipts' AND r.ordinal<=i.last_ordinal)
 OR (i.state='mapped' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0))
 OR EXISTS(SELECT 1 FROM catalog_enrichment_records r
 LEFT JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN catalog_enrichment_imports i ON i.snapshot_uuid=r.snapshot_uuid
 LEFT JOIN source_enrichment_receipts x ON x.uuid=r.receipt_uuid
 WHERE e.source_table IS NOT 'enrichment_receipts'
 OR (r.post_uuid IS NOT NULL AND r.post_uuid IS NOT (SELECT p.post_uuid FROM catalog_evidence_posts p
  WHERE p.snapshot_uuid=r.snapshot_uuid AND p.source_key=json_array(json_extract(e.data,'$.values.post_key'))))
 OR (r.receipt_uuid IS NOT NULL AND (x.uuid IS NULL OR x.post_uuid IS NOT r.post_uuid OR x.collection_uuid IS NOT i.collection_uuid
  OR x.collection_revision IS NOT i.collection_revision OR x.origin!='migration' OR x.policy IS NOT i.policy
  OR x.source_version IS NOT json_extract(e.data,'$.values.version') OR x.completed_at IS NOT json_extract(e.data,'$.values.completed_at'))))
 OR EXISTS(SELECT 1 FROM source_enrichment_receipts x WHERE NOT EXISTS(SELECT 1 FROM catalog_enrichment_records r WHERE r.receipt_uuid=x.uuid))`)
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid catalog enrichment progress or reference scope")
	}
	if err := validateCatalogEnrichmentTimes(conn); err != nil {
		return err
	}
	return validateCatalogEnrichmentAssertions(conn)
}

func validateCatalogEnrichmentTimes(conn *sqlx.DB) error {
	rows, err := conn.Query(`SELECT s.captured_at,i.created_at,i.updated_at FROM catalog_enrichment_imports i JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var captured, created, updated string
		if err := rows.Scan(&captured, &created, &updated); err != nil {
			return err
		}
		boundary, err := time.Parse(time.RFC3339Nano, captured)
		if err != nil {
			return err
		}
		start, err := time.Parse(time.RFC3339Nano, created)
		if err != nil || !validJobTime(start) || start.Before(boundary) {
			return errors.New("invalid catalog enrichment import creation time")
		}
		end, err := time.Parse(time.RFC3339Nano, updated)
		if err != nil || !validJobTime(end) || end.Before(start) {
			return errors.New("invalid catalog enrichment import update time")
		}
	}
	return rows.Err()
}

func validateCatalogEnrichmentAssertions(conn *sqlx.DB) error {
	rows, err := conn.Queryx(`SELECT x.*,e.data,e.data_sha256,s.source_uuid,s.catalog_id,s.captured_at,i.created_at,i.updated_at
 FROM catalog_enrichment_records r JOIN source_enrichment_receipts x ON x.uuid=r.receipt_uuid
 JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 JOIN catalog_snapshots s ON s.uuid=r.snapshot_uuid JOIN catalog_enrichment_imports i ON i.snapshot_uuid=r.snapshot_uuid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row struct {
			models.SourceEnrichmentReceipt
			Data     string `db:"data"`
			SHA256   string `db:"data_sha256"`
			Source   string `db:"source_uuid"`
			Catalog  string `db:"catalog_id"`
			Captured string `db:"captured_at"`
			Created  string `db:"created_at"`
			Updated  string `db:"updated_at"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		object, err := archive.DecodeJSONObject([]byte(row.Data), scrape.CatalogChunkLimit)
		if err != nil || scrape.CatalogSnapshotSHA([]byte(row.Data)) != row.SHA256 {
			return errors.New("invalid catalog enrichment source record")
		}
		values, ok := object["values"].(map[string]any)
		if !ok {
			return errors.New("invalid catalog enrichment source values")
		}
		boundary, err := time.Parse(time.RFC3339Nano, row.Captured)
		if err != nil {
			return err
		}
		prepared, reason := scrape.PrepareCatalogEnrichmentReceipt(values, boundary)
		if reason != "" || prepared.AttachmentLinksEnriched != row.AttachmentLinksEnriched || prepared.UnresolvedChildren != row.UnresolvedChildren {
			return errors.New("invalid catalog enrichment receipt assertion")
		}
		id, err := catalogEnrichmentReceiptID(row.Source, row.Catalog, row.CollectionUUID, row.PostUUID, prepared, row.CompletedAt)
		if err != nil || id != row.UUID {
			return errors.New("invalid catalog enrichment receipt identity")
		}
		recorded, err := time.Parse(time.RFC3339Nano, row.RecordedAt)
		// Recording time belongs to the import; completion time belongs to the old worker.
		created, createErr := time.Parse(time.RFC3339Nano, row.Created)
		updated, updateErr := time.Parse(time.RFC3339Nano, row.Updated)
		if err != nil || createErr != nil || updateErr != nil || !validJobTime(recorded) || recorded.Before(created) || recorded.After(updated) {
			return errors.New("invalid catalog enrichment receipt recording time")
		}
	}
	return rows.Err()
}
