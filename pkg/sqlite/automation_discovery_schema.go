package sqlite

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func validateAutomationDiscoverySchema(conn *sqlx.DB) error {
	for _, name := range []string{"automation_discovery_input", "automation_discovery_imports", "automation_discovery_import_guard",
		"automation_discovery_records", "automation_discovery_review", "automation_discovery_accounts", "automation_discovery_posts",
		"automation_discovery_record_immutable", "automation_discovery_record_scope"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM automation_discovery_imports i
 LEFT JOIN automation_snapshots s ON s.uuid=i.snapshot_uuid
 LEFT JOIN automation_enrichment_imports p ON p.snapshot_uuid=i.snapshot_uuid
 WHERE s.state IS NOT 'received' OR s.manifest_sha256 IS NOT i.manifest_sha256 OR i.policy!='automation-discovery-v1'
 OR p.state IS NULL OR p.state='running' OR p.manifest_sha256 IS NOT i.manifest_sha256
 OR i.source_records!=(SELECT count(*) FROM automation_snapshot_records e INDEXED BY automation_discovery_input WHERE e.snapshot_uuid=i.snapshot_uuid AND `+automationDiscoveryFamilies+`)
 OR i.processed_records!=(SELECT count(*) FROM automation_discovery_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.mapped_records!=(SELECT count(*) FROM automation_discovery_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='mapped')
 OR i.review_records!=(SELECT count(*) FROM automation_discovery_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR i.last_ordinal!=(SELECT coalesce(max(ordinal),0) FROM automation_discovery_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.processed_records!=(SELECT count(*) FROM automation_snapshot_records e INDEXED BY automation_discovery_input
  WHERE e.snapshot_uuid=i.snapshot_uuid AND e.ordinal<=i.last_ordinal AND `+automationDiscoveryFamilies+`))`)
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("invalid automation discovery progress")
	}
	var times []struct{ Captured, Created, Updated, Enriched string }
	if err := conn.Select(&times, `SELECT s.captured_at AS captured,i.created_at AS created,i.updated_at AS updated,p.updated_at AS enriched
 FROM automation_discovery_imports i JOIN automation_snapshots s ON s.uuid=i.snapshot_uuid
 JOIN automation_enrichment_imports p ON p.snapshot_uuid=i.snapshot_uuid`); err != nil {
		return err
	}
	for _, row := range times {
		captured, a := time.Parse(time.RFC3339Nano, row.Captured)
		created, b := time.Parse(time.RFC3339Nano, row.Created)
		updated, c := time.Parse(time.RFC3339Nano, row.Updated)
		enriched, d := time.Parse(time.RFC3339Nano, row.Enriched)
		if a != nil || b != nil || c != nil || d != nil || !validJobTime(created) || !validJobTime(updated) ||
			created.Before(captured) || created.Before(enriched) || updated.Before(created) {
			return errors.New("invalid automation discovery import time")
		}
	}
	if err := validateDiscoveryBindings(conn); err != nil {
		return err
	}
	return validateDiscoveryProjections(conn)
}

func validateDiscoveryBindings(conn *sqlx.DB) error {
	var invalid bool
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM automation_discovery_records r
 JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 JOIN automation_snapshots s ON s.uuid=r.snapshot_uuid
 LEFT JOIN automation_snapshot_records a ON a.snapshot_uuid=r.snapshot_uuid AND a.ordinal=r.account_ordinal
 LEFT JOIN automation_snapshot_records t ON t.snapshot_uuid=r.snapshot_uuid AND t.ordinal=r.target_ordinal
 LEFT JOIN automation_enrichment_records en ON en.snapshot_uuid=r.snapshot_uuid AND en.ordinal=r.enrichment_ordinal
 LEFT JOIN automation_snapshot_records er ON er.snapshot_uuid=r.snapshot_uuid AND er.ordinal=r.enrichment_ordinal
 LEFT JOIN source_post_identifiers p ON p.namespace=('legacy:catalog:'||s.source_uuid) AND p.value=r.post_reference
 LEFT JOIN catalog_collection_mappings m ON m.source_uuid=s.source_uuid AND m.import_uuid=s.registry_import_uuid AND m.catalog_id=json_extract(e.data,'$.values.catalog_id')
 LEFT JOIN catalog_snapshots c ON c.uuid=r.catalog_snapshot_uuid
 LEFT JOIN source_accounts account ON account.uuid=r.account_uuid
 LEFT JOIN catalog_account_mappings am ON am.source_uuid=s.source_uuid AND am.import_uuid=s.registry_import_uuid
  AND am.account_key=CASE e.source_table WHEN 'discovery_accounts' THEN json_extract(e.data,'$.values.account_key')
   ELSE json_extract(a.data,'$.values.account_key') END
 WHERE e.source_table NOT IN ('discovery_accounts','discovery_targets','discovery_candidates','maintenance')
 OR (r.post_uuid IS NOT NULL AND p.post_uuid IS NOT r.post_uuid)
 OR (r.collection_uuid IS NOT NULL AND (r.collection_uuid IS NOT m.collection_uuid OR r.collection_revision!=1))
 OR (r.catalog_snapshot_uuid IS NOT NULL AND (c.source_uuid IS NOT s.source_uuid OR c.catalog_id IS NOT json_extract(e.data,'$.values.catalog_id') OR c.collection_uuid IS NOT r.collection_uuid))
 OR (r.account_uuid IS NOT NULL AND (am.account_uuid IS NOT r.account_uuid OR account.namespace IS NOT ('native:'||CASE e.source_table
  WHEN 'discovery_accounts' THEN json_extract(e.data,'$.values.platform') ELSE json_extract(a.data,'$.values.platform') END)))
 OR (r.account_ordinal IS NOT NULL AND (a.source_table IS NOT 'discovery_accounts' OR json_extract(a.data,'$.values.job_key') IS NOT
  CASE e.source_table WHEN 'discovery_candidates' THEN json_extract(t.data,'$.values.job_key') ELSE json_extract(e.data,'$.values.job_key') END))
 OR (r.target_ordinal IS NOT NULL AND (t.source_table IS NOT 'discovery_targets' OR
  (e.source_table='discovery_candidates' AND (json_extract(t.data,'$.values.catalog_id') IS NOT json_extract(e.data,'$.values.catalog_id')
    OR json_extract(t.data,'$.values.post_key') IS NOT json_extract(e.data,'$.values.post_key')))))
 OR (r.enrichment_ordinal IS NOT NULL AND (er.source_table IS NOT 'enrichment_jobs' OR json_extract(er.data,'$.values.version') IS NOT 1
  OR json_extract(er.data,'$.values.catalog_id') IS NOT json_extract(e.data,'$.values.catalog_id')
  OR json_extract(er.data,'$.values.post_key') IS NOT json_extract(e.data,'$.values.post_key')))
 OR (r.outcome='mapped' AND r.enrichment_ordinal IS NOT NULL AND (en.outcome IS NOT 'mapped' OR en.post_uuid IS NOT r.post_uuid
  OR en.collection_uuid IS NOT r.collection_uuid OR en.collection_revision IS NOT r.collection_revision))
 OR (r.disposition='historical_completion' AND en.receipt_uuid IS NULL AND en.capture_uuid IS NULL)
 OR (r.disposition='source_present' AND en.capture_uuid IS NULL)
 OR (r.disposition='coalesced' AND (en.alias_ordinal IS NULL OR en.disposition IS NOT 'coalesced'))
 OR (r.disposition='lookup' AND (en.target_uuid IS NULL OR NOT EXISTS(SELECT 1 FROM source_post_urls u WHERE u.uuid=en.url_uuid AND u.url=r.candidate_url AND u.post_uuid=r.post_uuid)))
 OR (r.disposition='held' AND e.source_table='discovery_targets' AND (r.account_ordinal IS NULL OR r.post_uuid IS NULL))
 OR (r.disposition='candidate' AND r.account_ordinal IS NULL))`)
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("invalid automation discovery identity or proof scope")
	}
	return nil
}

func validateDiscoveryProjections(conn *sqlx.DB) error {
	rows, err := conn.Queryx(`SELECT ` + automationDiscoveryColumns + `,e.data,s.captured_at,s.source_uuid,
 pc.cooldown_until AS platform_until,ac.cooldown_until AS account_until
 ` + automationDiscoveryJoins + ` JOIN automation_snapshots s ON s.uuid=r.snapshot_uuid
 LEFT JOIN automation_enrichment_records pc ON pc.snapshot_uuid=r.snapshot_uuid AND pc.disposition='cooldown'
  AND pc.cooldown_kind='platform' AND pc.cooldown_value=json_extract(e.data,'$.values.platform')
 LEFT JOIN automation_enrichment_records ac ON ac.snapshot_uuid=r.snapshot_uuid AND ac.disposition='cooldown'
  AND ac.cooldown_kind='account' AND ac.cooldown_value=json_extract(e.data,'$.values.account_key')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row struct {
			models.AutomationDiscoveryRecord
			Data          string     `db:"data"`
			Captured      string     `db:"captured_at"`
			Source        string     `db:"source_uuid"`
			PlatformUntil *time.Time `db:"platform_until"`
			AccountUntil  *time.Time `db:"account_until"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		tree, err := archive.DecodeJSONObject([]byte(row.Data), scrape.CatalogChunkLimit)
		if err != nil || scrape.CatalogSnapshotSHA([]byte(row.Data)) != row.SHA256 {
			return models.ErrAutomationSnapshotInvalid
		}
		values, ok := tree["values"].(map[string]any)
		captured, err := time.Parse(time.RFC3339Nano, row.Captured)
		if !ok || err != nil {
			return models.ErrAutomationSnapshotInvalid
		}
		prepared, reason := scrape.PrepareAutomationDiscovery(row.Table, values, captured)
		if reason != "" {
			if row.Outcome != "review" || row.Disposition != "review" || row.Reason != reason ||
				row.AccountUUID != nil || row.AccountOrdinal != nil || row.TargetOrdinal != nil || row.EnrichmentOrdinal != nil ||
				row.PostUUID != nil || row.PostReference != "" || row.CollectionUUID != nil || row.CatalogSnapshotUUID != nil ||
				!reflect.DeepEqual(row.AutomationDiscoveryProjection, models.AutomationDiscoveryProjection{}) {
				return models.ErrAutomationSnapshotInvalid
			}
			continue
		}
		projection := prepared.Projection
		if projection.NotBefore != nil && row.AccountUUID != nil {
			for _, until := range []*time.Time{row.PlatformUntil, row.AccountUntil} {
				if until != nil && until.After(*projection.NotBefore) {
					projection.NotBefore = until
				}
			}
		}
		// Marshal times in UTC so database driver location objects cannot change
		// equality. Original timestamp text remains in the immutable source row.
		canonical := func(p models.AutomationDiscoveryProjection) (string, error) {
			if p.NotBefore != nil {
				at := p.NotBefore.UTC()
				p.NotBefore = &at
			}
			if p.MaintenanceTime != nil {
				at := p.MaintenanceTime.UTC()
				p.MaintenanceTime = &at
			}
			encoded, err := json.Marshal(p)
			return string(encoded), err
		}
		expected, expectedErr := canonical(projection)
		actual, actualErr := canonical(row.AutomationDiscoveryProjection)
		if expectedErr != nil || actualErr != nil || expected != actual ||
			(row.Outcome == "mapped" && row.Disposition != prepared.Disposition) {
			return models.ErrAutomationSnapshotInvalid
		}
		if row.PostReference != "" {
			ref, err := scrape.CatalogLocalPostReference(row.Source, prepared.CatalogID, prepared.PostKey)
			if err != nil || ref.Value != row.PostReference {
				return models.ErrAutomationSnapshotInvalid
			}
		}
		if row.Table == "maintenance" && (row.AccountUUID != nil || row.AccountOrdinal != nil || row.TargetOrdinal != nil || row.EnrichmentOrdinal != nil || row.PostUUID != nil || row.PostReference != "" || row.CollectionUUID != nil || row.CatalogSnapshotUUID != nil) {
			return models.ErrAutomationSnapshotInvalid
		}
	}
	return rows.Err()
}
