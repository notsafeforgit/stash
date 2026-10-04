package sqlite

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func validateSourceFileHistorySchema(conn *sqlx.DB) error {
	for _, name := range []string{"source_file_history", "source_file_history_locations", "source_file_history_edits", "source_file_history_states", "source_file_history_deduplications",
		"source_file_history_reference", "source_file_history_collection", "source_file_history_observation", "source_file_history_claim",
		"source_file_history_immutable", "source_file_history_location_immutable", "source_file_history_location_scope", "source_file_history_edit_immutable",
		"source_file_history_edit_scope", "source_file_history_state_immutable", "source_file_history_state_scope", "source_file_history_deduplication_immutable", "source_file_history_deduplication_scope",
		"catalog_file_history_imports", "catalog_file_history_records", "catalog_file_history_import_guard", "catalog_file_history_record_immutable", "catalog_file_history_review", "catalog_file_history_event"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_file_history h
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=h.collection_uuid AND c.revision=h.collection_revision
 LEFT JOIN media_root_revisions r ON r.root_uuid=h.root_uuid AND r.revision=h.root_revision
 WHERE c.collection_uuid IS NULL OR r.root_uuid IS NULL)`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	for _, table := range []string{"locations", "edits", "states", "deduplications"} {
		if err := conn.Get(&invalid, "SELECT EXISTS(SELECT 1 FROM source_file_history_"+table+" x LEFT JOIN source_file_history h ON h.uuid=x.history_uuid WHERE h.uuid IS NULL)"); err != nil {
			return err
		}
		if invalid {
			return models.ErrSourcePayloadCorrupt
		}
	}
	for after := ""; ; {
		var id string
		if err := conn.Get(&id, "SELECT coalesce(min(uuid),'') FROM source_file_history WHERE uuid>?", after); err != nil {
			return err
		}
		if id == "" {
			break
		}
		if _, err := readSourceFileHistory(conn.Get, conn.Select, id); err != nil {
			return fmt.Errorf("source file history %s: %w", id, err)
		}
		after = id
	}
	return validateCatalogFileHistorySchema(conn)
}

func validateCatalogFileHistorySchema(conn *sqlx.DB) error {
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM catalog_file_history_imports i
 LEFT JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid LEFT JOIN catalog_media_imports m ON m.snapshot_uuid=i.snapshot_uuid
 WHERE s.state IS NOT 'received' OR s.manifest_sha256 IS NOT i.manifest_sha256 OR s.collection_uuid IS NOT i.collection_uuid
 OR m.state IS NULL OR m.state='running' OR m.manifest_sha256 IS NOT i.manifest_sha256
 OR m.collection_revision IS NOT i.collection_revision OR m.root_uuid IS NOT i.root_uuid OR m.root_revision IS NOT i.root_revision
 OR i.policy!='catalog-file-history-v1'
 OR i.source_records!=(SELECT count(*) FROM catalog_snapshot_records e WHERE e.snapshot_uuid=i.snapshot_uuid AND e.source_table IN `+catalogFileHistoryTables+`)
 OR i.processed_records!=(SELECT count(*) FROM catalog_file_history_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.mapped_records!=(SELECT count(*) FROM catalog_file_history_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='mapped')
 OR i.review_records!=(SELECT count(*) FROM catalog_file_history_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR i.last_ordinal!=(SELECT coalesce(max(ordinal),0) FROM catalog_file_history_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.processed_records!=(SELECT count(*) FROM catalog_snapshot_records e WHERE e.snapshot_uuid=i.snapshot_uuid AND e.source_table IN `+catalogFileHistoryTables+` AND e.ordinal<=i.last_ordinal)
 OR (i.state='mapped' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0))
 OR EXISTS(SELECT 1 FROM catalog_file_history_records r LEFT JOIN catalog_file_history_imports i ON i.snapshot_uuid=r.snapshot_uuid
 LEFT JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN source_file_history h ON h.uuid=r.history_uuid
 WHERE i.snapshot_uuid IS NULL OR e.source_table IS NULL OR e.source_table NOT IN `+catalogFileHistoryTables+`
 OR (r.history_uuid IS NOT NULL AND (h.uuid IS NULL OR h.collection_uuid IS NOT i.collection_uuid OR h.collection_revision IS NOT i.collection_revision
 OR h.root_uuid IS NOT i.root_uuid OR h.root_revision IS NOT i.root_revision OR h.origin IS NOT 'migration')))`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	for after := ""; ; {
		var id string
		if err := conn.Get(&id, "SELECT coalesce(min(history_uuid),'') FROM catalog_file_history_records WHERE history_uuid>?", after); err != nil {
			return err
		}
		if id == "" {
			return nil
		}
		if err := verifyCatalogFileHistoryRecord(conn, id); err != nil {
			return fmt.Errorf("catalog file history %s: %w", id, err)
		}
		after = id
	}
}

func catalogHistoryMappedObservation(conn *sqlx.DB, snapshot, path string) (*string, error) {
	key, err := archive.EncodeSourceJSON([]string{path})
	if err != nil {
		return nil, err
	}
	var ret *string
	err = conn.Get(&ret, `SELECT r.observation_uuid FROM catalog_media_records r JOIN catalog_snapshot_records e
 ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal WHERE e.snapshot_uuid=? AND e.source_table='files' AND e.source_key=?`, snapshot, string(key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func verifyCatalogFileHistoryRecord(conn *sqlx.DB, id string) error {
	var row struct {
		Snapshot   string `db:"snapshot_uuid"`
		Ordinal    int64  `db:"ordinal"`
		Table      string `db:"source_table"`
		Data       string `db:"data"`
		SHA        string `db:"data_sha256"`
		Source     string `db:"source_uuid"`
		Catalog    string `db:"catalog_id"`
		CapturedAt string `db:"captured_at"`
		Outcome    string `db:"outcome"`
		Reason     string `db:"reason"`
	}
	if err := conn.Get(&row, `SELECT r.snapshot_uuid,r.ordinal,e.source_table,e.data,e.data_sha256,s.source_uuid,s.catalog_id,s.captured_at,r.outcome,r.reason
 FROM catalog_file_history_records r JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 JOIN catalog_snapshots s ON s.uuid=r.snapshot_uuid WHERE r.history_uuid=?`, id); err != nil {
		return err
	}
	history, err := readSourceFileHistory(conn.Get, conn.Select, id)
	if err != nil || history == nil || id != scrape.RegistryImportUUID(row.Snapshot, "catalog-file-history:v1", strconv.FormatInt(row.Ordinal, 10)) {
		return models.ErrSourcePayloadCorrupt
	}
	object, err := archive.DecodeJSONObject([]byte(row.Data), scrape.CatalogChunkLimit)
	if err != nil || scrape.CatalogSnapshotSHA([]byte(row.Data)) != row.SHA {
		return models.ErrSourcePayloadCorrupt
	}
	v, ok := object["values"].(map[string]any)
	if !ok {
		return models.ErrSourcePayloadCorrupt
	}
	stamp, err := time.Parse(time.RFC3339Nano, row.CapturedAt)
	if err != nil || !stamp.Equal(history.ObservedAt) || history.ReferenceNamespace != "legacy:catalog:"+row.Source+":"+row.Catalog {
		return models.ErrSourcePayloadCorrupt
	}
	details, err := archive.EncodeSourceJSON(map[string]any{"snapshot_uuid": row.Snapshot, "source_ordinal": row.Ordinal, "source_sha256": row.SHA})
	if err != nil || !bytes.Equal(history.Details, details) {
		return models.ErrSourcePayloadCorrupt
	}
	key, timeKey, kind := "event_id", "created_at", "deduplication"
	switch row.Table {
	case "metadata_edits":
		key, kind = "edit_id", "metadata_edit"
	case "file_events":
		timeKey, kind = "observed_at", "state_change"
	}
	if v[key] != history.ReferenceValue || v[timeKey] != history.SourceTime || history.Kind != kind {
		return models.ErrSourcePayloadCorrupt
	}
	if kind != "deduplication" {
		path, ok := v["relpath"].(string)
		if !ok {
			return models.ErrSourcePayloadCorrupt
		}
		observation, err := catalogHistoryMappedObservation(conn, row.Snapshot, path)
		if err != nil || observation == nil || !reflect.DeepEqual(observation, history.Locations[0].ObservationUUID) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	review := ""
	switch kind {
	case "metadata_edit":
		body, ok := v["fields_json"].(string)
		if !ok {
			return models.ErrSourcePayloadCorrupt
		}
		edits, err := archive.CatalogFileEdits([]byte(body))
		if err != nil || !reflect.DeepEqual(edits, history.Edits) {
			return models.ErrSourcePayloadCorrupt
		}
		for _, edit := range edits {
			if edit.Mode == "unmapped" {
				review = "metadata_fields_require_review"
			}
		}
	case "state_change":
		state := history.StateChange
		if v["old_state"] != state.OldState || v["new_state"] != state.NewState || v["reason"] != state.Reason {
			return models.ErrSourcePayloadCorrupt
		}
	case "deduplication":
		dedupe := history.Deduplication
		survivor, ok := catalogMediaText(v["survivor_relpath"])
		if !ok || !reflect.DeepEqual(survivor, dedupe.SurvivorPath) || v["stage"] != dedupe.Stage {
			return models.ErrSourcePayloadCorrupt
		}
		key, err := archive.EncodeSourceJSON([]any{v["asset_id"]})
		if err != nil {
			return err
		}
		var claim *string
		if err := conn.Get(&claim, `SELECT r.claim_uuid FROM catalog_media_records r JOIN catalog_snapshot_records e
 ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal WHERE e.snapshot_uuid=? AND e.source_table='assets' AND e.source_key=?`, row.Snapshot, string(key)); err != nil {
			return err
		}
		if claim == nil || *claim != dedupe.ContentClaimUUID {
			return models.ErrSourcePayloadCorrupt
		}
		pathsJSON, ok := v["paths_json"].(string)
		if !ok {
			return models.ErrSourcePayloadCorrupt
		}
		wrapped, err := archive.DecodeJSONObject([]byte(`{"paths":`+pathsJSON+`}`), archive.MaxFileHistoryEditBytes)
		if err != nil {
			return err
		}
		paths, ok := wrapped["paths"].([]any)
		if !ok || len(paths) != len(history.Locations) {
			return models.ErrSourcePayloadCorrupt
		}
		for i, location := range history.Locations {
			path := sourceFileHistoryPath(location)
			if paths[i] != path {
				return models.ErrSourcePayloadCorrupt
			}
			expected, err := catalogHistoryMappedObservation(conn, row.Snapshot, path)
			if err != nil {
				return err
			}
			if expected != nil {
				var matches bool
				if err := conn.Get(&matches, "SELECT EXISTS(SELECT 1 FROM source_file_observations WHERE uuid=? AND content_claim_uuid=?)", *expected, dedupe.ContentClaimUUID); err != nil {
					return err
				}
				if !matches {
					expected = nil
				}
			}
			if !reflect.DeepEqual(expected, location.ObservationUUID) {
				return models.ErrSourcePayloadCorrupt
			}
			if expected == nil {
				review = "deduplication_member_requires_review"
			}
		}
	}
	if row.Reason != review || (row.Outcome == "review") != (review != "") {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}
