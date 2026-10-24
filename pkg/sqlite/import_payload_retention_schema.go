package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateImportPayloadRetentionSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"catalog_payload_retirements", "catalog_retired_payloads", "catalog_retirable_snapshots", "catalog_retirable_payloads", "catalog_payload_retirement_begin", "catalog_payload_retirement_progress", "catalog_payload_retirement_retained", "catalog_payload_retirement_count"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var columns int
	if err := conn.Get(&columns, "SELECT count(*) FROM pragma_table_info('catalog_snapshot_records') WHERE name IN ('original_byte_count','retained_asset_id')"); err != nil {
		return err
	}
	if columns != 2 {
		return fmt.Errorf("native database is missing catalog payload retention state")
	}
	if !auditData {
		return nil
	}
	var invalid bool
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM catalog_payload_retirements p
JOIN catalog_snapshots s ON s.uuid=p.snapshot_uuid WHERE p.manifest_sha256<>s.manifest_sha256
 OR NOT EXISTS(SELECT 1 FROM catalog_retirable_snapshots e WHERE e.uuid=p.snapshot_uuid)
 OR p.retired_records<>(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=p.snapshot_uuid AND r.original_byte_count IS NOT NULL)
 OR p.released_bytes<>(SELECT coalesce(sum(r.original_byte_count-r.byte_count),0) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=p.snapshot_uuid AND r.original_byte_count IS NOT NULL)
 OR EXISTS(SELECT 1 FROM catalog_snapshot_records r WHERE r.snapshot_uuid=p.snapshot_uuid AND r.original_byte_count IS NOT NULL AND r.ordinal>p.last_ordinal)
 OR (p.state='complete' AND (p.last_ordinal<>s.received_records OR EXISTS(SELECT 1 FROM catalog_retirable_payloads r WHERE r.snapshot_uuid=p.snapshot_uuid))))
 OR EXISTS(SELECT 1 FROM catalog_snapshot_records r WHERE r.original_byte_count IS NOT NULL AND (
 r.original_byte_count NOT BETWEEN 2 AND 16777216 OR r.data<>'{}' OR r.byte_count<>2
 OR (r.source_table='assets' AND r.retained_asset_id IS NOT json_extract(r.source_key,'$[0]'))
 OR r.source_table NOT IN ('account_snapshots','observations','observation_details','assets','files','appearances')
 OR NOT EXISTS(SELECT 1 FROM catalog_payload_retirements p WHERE p.snapshot_uuid=r.snapshot_uuid)))`)
	if err != nil {
		return err
	}
	if invalid {
		return fmt.Errorf("native database has invalid catalog payload retirement receipts")
	}
	return nil
}
