package sqlite

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func validateCatalogCleanupSchema(conn *sqlx.DB) error {
	for _, name := range []string{"source_cleanup_intents", "source_cleanup_intents_collection", "source_cleanup_intent_immutable", "source_cleanup_intent_scope",
		"catalog_cleanup_imports", "catalog_cleanup_import_guard", "catalog_cleanup_records", "catalog_cleanup_review", "catalog_cleanup_intent_source", "catalog_cleanup_record_immutable"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM catalog_cleanup_imports i
 LEFT JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid LEFT JOIN catalog_evidence_imports e ON e.snapshot_uuid=i.snapshot_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=i.collection_uuid AND c.revision=i.collection_revision
 WHERE s.state IS NOT 'received' OR s.manifest_sha256 IS NOT i.manifest_sha256 OR s.collection_uuid IS NOT i.collection_uuid
 OR e.state IS NULL OR e.state='running' OR e.manifest_sha256 IS NOT i.manifest_sha256
 OR i.policy!='catalog-cleanup-v1' OR i.collection_revision!=1 OR c.origin IS NOT 'migration'
 OR i.source_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.source_table='metadata_prune_queue')
 OR i.processed_records!=(SELECT count(*) FROM catalog_cleanup_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.held_records!=(SELECT count(*) FROM catalog_cleanup_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='held')
 OR i.review_records!=(SELECT count(*) FROM catalog_cleanup_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR i.last_ordinal!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_cleanup_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.processed_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.source_table='metadata_prune_queue' AND r.ordinal<=i.last_ordinal)
 OR (i.state!='running' AND i.processed_records!=i.source_records)
 OR (i.state='retained' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0))
 OR EXISTS(SELECT 1 FROM catalog_cleanup_records r LEFT JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 WHERE e.source_table IS NOT 'metadata_prune_queue')
 OR EXISTS(SELECT 1 FROM source_cleanup_intents x WHERE NOT EXISTS(SELECT 1 FROM catalog_cleanup_records r WHERE r.intent_uuid=x.uuid))`)
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid catalog cleanup progress or scope")
	}
	if err := validateCatalogCleanupTimes(conn); err != nil {
		return err
	}
	return validateCatalogCleanupAssertions(conn)
}

func validateCatalogCleanupTimes(conn *sqlx.DB) error {
	rows, err := conn.Query(`SELECT s.captured_at,i.created_at,i.updated_at FROM catalog_cleanup_imports i JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid`)
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
			return errors.New("invalid catalog cleanup import creation time")
		}
		end, err := time.Parse(time.RFC3339Nano, updated)
		if err != nil || !validJobTime(end) || end.Before(start) {
			return errors.New("invalid catalog cleanup import update time")
		}
	}
	return rows.Err()
}

func validateCatalogCleanupAssertions(conn *sqlx.DB) error {
	rows, err := conn.Queryx(`SELECT r.snapshot_uuid,r.ordinal,r.intent_uuid,r.outcome,r.reason,e.data,e.data_sha256,
 s.source_uuid,s.catalog_id,s.captured_at,i.collection_uuid,i.created_at,i.updated_at,
 CASE WHEN x.uuid IS NULL THEN NULL ELSE json_object('uuid',x.uuid,'collection_uuid',x.collection_uuid,'collection_revision',x.collection_revision,
 'kind',x.kind,'state',x.state,'reference_namespace',x.reference_namespace,'reference_value',x.reference_value,'requested_at',x.requested_at,
 'origin',x.origin,'recorded_at',x.recorded_at) END AS intent_json
 FROM catalog_cleanup_records r JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 JOIN catalog_snapshots s ON s.uuid=r.snapshot_uuid JOIN catalog_cleanup_imports i ON i.snapshot_uuid=r.snapshot_uuid
 LEFT JOIN source_cleanup_intents x ON x.uuid=r.intent_uuid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row struct {
			Snapshot   string  `db:"snapshot_uuid"`
			Ordinal    int64   `db:"ordinal"`
			IntentUUID *string `db:"intent_uuid"`
			Outcome    string  `db:"outcome"`
			Reason     string  `db:"reason"`
			Data       string  `db:"data"`
			SHA256     string  `db:"data_sha256"`
			Source     string  `db:"source_uuid"`
			Catalog    string  `db:"catalog_id"`
			Captured   string  `db:"captured_at"`
			Collection string  `db:"collection_uuid"`
			Created    string  `db:"created_at"`
			Updated    string  `db:"updated_at"`
			IntentJSON *string `db:"intent_json"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		object, err := archive.DecodeJSONObject([]byte(row.Data), scrape.CatalogChunkLimit)
		if err != nil || scrape.CatalogSnapshotSHA([]byte(row.Data)) != row.SHA256 {
			return models.ErrSourcePayloadCorrupt
		}
		values, ok := object["values"].(map[string]any)
		if !ok {
			return models.ErrSourcePayloadCorrupt
		}
		boundary, err := time.Parse(time.RFC3339Nano, row.Captured)
		if err != nil {
			return err
		}
		prepared, reason := scrape.PrepareCatalogCleanupIntent(values, boundary)
		if reason != "" {
			if row.Outcome != "review" || row.Reason != reason || row.IntentUUID != nil || row.IntentJSON != nil {
				return models.ErrSourcePayloadCorrupt
			}
			continue
		}
		if row.Outcome != "held" || row.Reason != "" || row.IntentUUID == nil || row.IntentJSON == nil {
			return models.ErrSourcePayloadCorrupt
		}
		var intent models.SourceCleanupIntent
		if err := json.Unmarshal([]byte(*row.IntentJSON), &intent); err != nil {
			return err
		}
		if intent.UUID != catalogCleanupIntentID(row.Snapshot, row.Ordinal) || intent.UUID != *row.IntentUUID ||
			intent.CollectionUUID != row.Collection || intent.CollectionRevision != 1 || intent.Kind != "background_targets" || intent.State != "held" ||
			intent.ReferenceNamespace != "legacy:catalog:"+row.Source+":"+row.Catalog || intent.ReferenceValue != prepared.PostKey ||
			intent.RequestedAt != prepared.RequestedAt || intent.Origin != "migration" {
			return models.ErrSourcePayloadCorrupt
		}
		recorded, err := time.Parse(time.RFC3339Nano, intent.RecordedAt)
		created, createErr := time.Parse(time.RFC3339Nano, row.Created)
		updated, updateErr := time.Parse(time.RFC3339Nano, row.Updated)
		if err != nil || createErr != nil || updateErr != nil || !validJobTime(recorded) || recorded.Before(created) || recorded.After(updated) {
			return errors.New("invalid catalog cleanup intent recording time")
		}
	}
	return rows.Err()
}
