package sqlite

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

func validateCatalogSnapshotSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"catalog_snapshots", "catalog_snapshot_tables", "catalog_snapshot_chunks", "catalog_snapshot_records", "catalog_snapshot_identity_immutable", "catalog_snapshot_chunk_immutable", "catalog_snapshot_record_immutable", "catalog_snapshot_record_chunk"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	query := `SELECT EXISTS(SELECT 1 FROM catalog_snapshots s WHERE
 s.uuid IS NOT json_extract(CAST(s.manifest AS TEXT),'$.snapshot_uuid')
 OR s.source_uuid IS NOT json_extract(CAST(s.manifest AS TEXT),'$.registry_source_uuid')
 OR s.catalog_id IS NOT json_extract(CAST(s.manifest AS TEXT),'$.catalog_id')
 OR s.captured_at IS NOT json_extract(CAST(s.manifest AS TEXT),'$.captured_at')
 OR s.record_count IS NOT json_extract(CAST(s.manifest AS TEXT),'$.records')
 OR s.chunk_count IS NOT json_array_length(CAST(s.manifest AS TEXT),'$.chunks')
 OR NOT EXISTS(SELECT 1 FROM catalog_collection_mappings m WHERE m.source_uuid=s.source_uuid AND m.catalog_id=s.catalog_id AND m.import_uuid=s.registry_import_uuid AND m.collection_uuid=s.collection_uuid)
 OR s.next_chunk!=(SELECT count(*) FROM catalog_snapshot_chunks c WHERE c.snapshot_uuid=s.uuid)
 OR s.next_chunk!=(SELECT coalesce(max(c.chunk_index)+1,0) FROM catalog_snapshot_chunks c WHERE c.snapshot_uuid=s.uuid)
 OR s.received_records!=(SELECT coalesce(sum(c.record_count),0) FROM catalog_snapshot_chunks c WHERE c.snapshot_uuid=s.uuid)
 OR s.received_bytes!=(SELECT coalesce(sum(c.byte_count),0) FROM catalog_snapshot_chunks c WHERE c.snapshot_uuid=s.uuid)
 OR s.received_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=s.uuid)
 OR s.received_records!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=s.uuid)
 OR (SELECT count(*) FROM catalog_snapshot_tables t WHERE t.snapshot_uuid=s.uuid)!=(SELECT count(*) FROM json_each(CAST(s.manifest AS TEXT),'$.tables')))
 OR EXISTS(SELECT 1 FROM catalog_snapshot_chunks c JOIN catalog_snapshots s ON s.uuid=c.snapshot_uuid WHERE
 c.sha256 IS NOT json_extract(CAST(s.manifest AS TEXT),'$.chunks['||c.chunk_index||'].sha256')
 OR c.record_count IS NOT json_extract(CAST(s.manifest AS TEXT),'$.chunks['||c.chunk_index||'].rows')
 OR c.byte_count IS NOT json_extract(CAST(s.manifest AS TEXT),'$.chunks['||c.chunk_index||'].bytes')
 OR c.record_count!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=c.snapshot_uuid AND r.chunk_index=c.chunk_index))
 OR EXISTS(SELECT 1 FROM catalog_snapshot_tables t JOIN catalog_snapshots s ON s.uuid=t.snapshot_uuid WHERE
 NOT EXISTS(SELECT 1 FROM json_each(CAST(s.manifest AS TEXT),'$.tables') expected WHERE expected.key=t.source_table)
 OR t.received_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=t.snapshot_uuid AND r.source_table=t.source_table)
 OR t.received_records>json_extract(CAST(s.manifest AS TEXT),'$.tables.'||t.source_table||'.rows')
 OR t.received_bytes>json_extract(CAST(s.manifest AS TEXT),'$.tables.'||t.source_table||'.bytes')
 OR (s.state='received' AND (t.received_records!=json_extract(CAST(s.manifest AS TEXT),'$.tables.'||t.source_table||'.rows')
 OR t.received_bytes!=json_extract(CAST(s.manifest AS TEXT),'$.tables.'||t.source_table||'.bytes')
 OR t.received_sha256!=json_extract(CAST(s.manifest AS TEXT),'$.tables.'||t.source_table||'.sha256'))))`
	// Receipt totals describe the original input. Compaction only permits
	// absent rows for the explicitly completed input families, not other tables.
	var conditions []string
	var uncompacted []string
	for _, family := range []struct{ flag, tables string }{
		{"nfo_compacted", "t.source_table IN " + nfoSourceTables},
		{"memberships_compacted", "t.source_table='memberships'"},
	} {
		var supported bool
		if err := conn.Get(&supported, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('catalog_snapshots') WHERE name=?)`, family.flag); err != nil {
			return err
		}
		if supported {
			conditions = append(conditions, "(s."+family.flag+"=1 AND "+family.tables+")")
			uncompacted = append(uncompacted, "s."+family.flag+"=0")
		}
	}
	if len(conditions) > 0 {
		discarded := strings.Join(conditions, " OR ")
		unchanged := strings.Join(uncompacted, " AND ")
		query = strings.ReplaceAll(query, "s.received_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=s.uuid)",
			"s.received_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=s.uuid)+(SELECT coalesce(sum(t.received_records),0) FROM catalog_snapshot_tables t WHERE t.snapshot_uuid=s.uuid AND ("+discarded+"))")
		query = strings.ReplaceAll(query, "OR s.received_records!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=s.uuid)",
			"OR ("+unchanged+" AND s.received_records!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=s.uuid)) OR EXISTS(SELECT 1 FROM catalog_snapshot_records r WHERE r.snapshot_uuid=s.uuid AND r.ordinal>s.received_records)")
		query = strings.ReplaceAll(query, "OR c.record_count!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=c.snapshot_uuid AND r.chunk_index=c.chunk_index)",
			"OR ("+unchanged+" AND c.record_count!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=c.snapshot_uuid AND r.chunk_index=c.chunk_index))")
		query = strings.ReplaceAll(query, "t.received_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=t.snapshot_uuid AND r.source_table=t.source_table)",
			"CASE WHEN "+discarded+" THEN 0 ELSE t.received_records END!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=t.snapshot_uuid AND r.source_table=t.source_table)")
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, query); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has incomplete catalog snapshot receipts or staging records")
	}
	return nil
}
