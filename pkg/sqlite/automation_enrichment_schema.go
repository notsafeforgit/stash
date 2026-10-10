package sqlite

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func validateAutomationEnrichmentSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"automation_enrichment_input", "automation_enrichment_imports", "automation_enrichment_import_guard",
		"automation_enrichment_records", "automation_enrichment_review", "automation_enrichment_targets", "automation_enrichment_completions",
		"automation_enrichment_record_immutable", "automation_enrichment_cooldowns", "enrichment_completion_legacy_receipt", "enrichment_completion_legacy_capture"} {
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
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM automation_enrichment_imports i LEFT JOIN automation_snapshots s ON s.uuid=i.snapshot_uuid
 WHERE s.state IS NOT 'received' OR s.manifest_sha256 IS NOT i.manifest_sha256 OR i.policy!='automation-enrichment-v1'
 OR i.source_records!=(SELECT count(*) FROM automation_snapshot_records e WHERE e.snapshot_uuid=i.snapshot_uuid
  AND e.source_table IN ('enrichment_cooldowns','enrichment_jobs','enrichment_seed_progress','enrichment_source_progress'))
 OR i.processed_records!=(SELECT count(*) FROM automation_enrichment_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.mapped_records!=(SELECT count(*) FROM automation_enrichment_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='mapped')
 OR i.review_records!=(SELECT count(*) FROM automation_enrichment_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR i.last_ordinal!=(SELECT coalesce(max(ordinal),0) FROM automation_enrichment_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.processed_records!=(SELECT count(*) FROM automation_snapshot_records e WHERE e.snapshot_uuid=i.snapshot_uuid AND e.ordinal<=i.last_ordinal
  AND e.source_table IN ('enrichment_cooldowns','enrichment_jobs','enrichment_seed_progress','enrichment_source_progress')))
 OR EXISTS(SELECT 1 FROM automation_enrichment_records r
 JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 JOIN automation_snapshots s ON s.uuid=r.snapshot_uuid
 LEFT JOIN source_post_identifiers p ON p.namespace=('legacy:catalog:'||s.source_uuid) AND p.value=r.post_reference
 LEFT JOIN catalog_collection_mappings m ON m.source_uuid=s.source_uuid AND m.import_uuid=s.registry_import_uuid AND m.catalog_id=json_extract(e.data,'$.values.catalog_id')
 LEFT JOIN catalog_snapshots c ON c.uuid=r.catalog_snapshot_uuid
 LEFT JOIN enrichment_targets t ON t.uuid=r.target_uuid LEFT JOIN enrichment_target_history h ON h.target_uuid=r.target_uuid AND h.revision=r.target_revision
 LEFT JOIN source_post_urls u ON u.uuid=r.url_uuid LEFT JOIN source_post_url_evidence ue ON ue.uuid=r.url_evidence_uuid
 LEFT JOIN enrichment_completions done ON done.uuid=r.completion_uuid
 LEFT JOIN source_enrichment_receipts receipt ON receipt.uuid=r.receipt_uuid
 LEFT JOIN source_captures capture ON capture.uuid=r.capture_uuid
 LEFT JOIN catalog_snapshot_records alias ON alias.snapshot_uuid=r.catalog_snapshot_uuid AND alias.ordinal=r.alias_ordinal
 WHERE e.source_table NOT IN ('enrichment_cooldowns','enrichment_jobs','enrichment_seed_progress','enrichment_source_progress')
 OR (r.post_uuid IS NOT NULL AND p.post_uuid IS NOT r.post_uuid)
 OR (r.collection_uuid IS NOT NULL AND (r.collection_uuid IS NOT m.collection_uuid OR r.collection_revision!=1))
 OR (r.catalog_snapshot_uuid IS NOT NULL AND (c.source_uuid IS NOT s.source_uuid OR c.catalog_id IS NOT json_extract(e.data,'$.values.catalog_id') OR c.collection_uuid IS NOT r.collection_uuid))
 OR (r.url_uuid IS NOT NULL AND (u.post_uuid IS NOT r.post_uuid OR u.url IS NOT json_extract(e.data,'$.values.url')))
 OR (r.url_evidence_uuid IS NOT NULL AND (ue.url_uuid IS NOT r.url_uuid OR ue.origin!='migration'
  OR ue.basis!='automation-enrichment-input' OR json_extract(ue.details,'$.snapshot_uuid') IS NOT r.snapshot_uuid
  OR json_extract(ue.details,'$.source_ordinal') IS NOT r.ordinal OR json_extract(ue.details,'$.policy') IS NOT 'automation-enrichment-v1'
  OR json_extract(ue.details,'$.time_basis') IS NOT 'snapshot-boundary'))
 OR (r.target_uuid IS NOT NULL AND (t.post_uuid IS NOT r.post_uuid OR t.collection_uuid IS NOT r.collection_uuid OR t.collection_revision IS NOT r.collection_revision
  OR t.url_uuid IS NOT r.url_uuid OR t.policy IS NOT 'gallery-dl-metadata-v1' OR h.target_uuid IS NULL))
 OR (r.disposition='held' AND (t.origin IS NOT 'migration' OR h.state IS NOT 'held' OR h.not_before IS NOT r.not_before
  OR h.priority<json_extract(e.data,'$.values.priority')))
 OR (r.disposition='excluded' AND r.target_uuid IS NOT NULL AND (h.state IS NOT 'excluded' OR h.reason IS NOT 'legacy_excluded_source'))
 OR (r.completion_uuid IS NOT NULL AND (done.target_uuid IS NOT r.target_uuid OR h.completion_uuid IS NOT r.completion_uuid
  OR h.state IS NOT 'completed' OR done.legacy_receipt_uuid IS NOT r.receipt_uuid OR done.legacy_capture_uuid IS NOT r.capture_uuid))
 OR (r.receipt_uuid IS NOT NULL AND (receipt.post_uuid IS NOT r.post_uuid OR receipt.collection_uuid IS NOT r.collection_uuid OR receipt.collection_revision IS NOT r.collection_revision))
 OR (r.capture_uuid IS NOT NULL AND (capture.post_uuid IS NOT r.post_uuid OR capture.origin IS NOT 'gallery-dl'
  OR NOT EXISTS(SELECT 1 FROM source_collection_captures b WHERE b.capture_uuid=r.capture_uuid AND b.collection_uuid=r.collection_uuid AND b.collection_revision=r.collection_revision)))
 OR (r.alias_ordinal IS NOT NULL AND (alias.source_table IS NOT 'post_aliases' OR json_extract(alias.data,'$.values.alias_key') IS NOT json_extract(e.data,'$.values.post_key')
  OR NOT EXISTS(SELECT 1 FROM catalog_relation_records p WHERE p.snapshot_uuid=r.catalog_snapshot_uuid AND p.ordinal=r.alias_ordinal AND p.post_uuid=r.post_uuid AND p.outcome='mapped'))))
 OR EXISTS(SELECT 1 FROM enrichment_completions e LEFT JOIN enrichment_targets t ON t.uuid=e.target_uuid
 LEFT JOIN source_enrichment_receipts r ON r.uuid=e.legacy_receipt_uuid LEFT JOIN source_captures c ON c.uuid=e.legacy_capture_uuid
 WHERE (e.legacy_receipt_uuid IS NOT NULL OR e.legacy_capture_uuid IS NOT NULL) AND (t.origin IS NOT 'migration' OR t.collection_revision!=1
  OR e.capture_count!=0 OR NOT EXISTS(SELECT 1 FROM automation_enrichment_records a WHERE a.completion_uuid=e.uuid AND a.target_uuid=e.target_uuid)
  OR NOT EXISTS(SELECT 1 FROM source_collection_revisions b WHERE b.collection_uuid=t.collection_uuid AND b.revision=1 AND b.origin='migration')
  OR (e.legacy_receipt_uuid IS NOT NULL AND (r.post_uuid IS NOT t.post_uuid OR r.collection_uuid IS NOT t.collection_uuid OR r.collection_revision IS NOT t.collection_revision))
  OR (e.legacy_capture_uuid IS NOT NULL AND (c.post_uuid IS NOT t.post_uuid OR c.origin IS NOT 'gallery-dl'
   OR NOT EXISTS(SELECT 1 FROM source_collection_captures b WHERE b.capture_uuid=c.uuid AND b.collection_uuid=t.collection_uuid AND b.collection_revision=t.collection_revision)))))`)
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("invalid automation enrichment progress or proof scope")
	}
	if err := validateAutomationEnrichmentTimes(conn); err != nil {
		return err
	}
	return validateAutomationEnrichmentValues(conn)
}

func validateAutomationEnrichmentTimes(conn *sqlx.DB) error {
	rows, err := conn.Query(`SELECT s.captured_at,i.created_at,i.updated_at FROM automation_enrichment_imports i JOIN automation_snapshots s ON s.uuid=i.snapshot_uuid`)
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
			return errors.New("invalid automation enrichment import creation time")
		}
		end, err := time.Parse(time.RFC3339Nano, updated)
		if err != nil || !validJobTime(end) || end.Before(start) {
			return errors.New("invalid automation enrichment import update time")
		}
	}
	return rows.Err()
}

func validateAutomationEnrichmentValues(conn *sqlx.DB) error {
	rows, err := conn.Queryx(`SELECT ` + automationEnrichmentColumns + `,r.snapshot_uuid,e.data,s.source_uuid,s.captured_at,
 pc.cooldown_until AS platform_until,ac.cooldown_until AS account_until,ue.observed_at AS url_observed_at
 FROM automation_enrichment_records r JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 JOIN automation_snapshots s ON s.uuid=r.snapshot_uuid
 LEFT JOIN automation_enrichment_records pc ON pc.snapshot_uuid=r.snapshot_uuid AND pc.disposition='cooldown'
  AND pc.cooldown_kind='platform' AND pc.cooldown_value=json_extract(e.data,'$.values.platform')
 LEFT JOIN automation_enrichment_records ac ON ac.snapshot_uuid=r.snapshot_uuid AND ac.disposition='cooldown'
  AND ac.cooldown_kind='account' AND ac.cooldown_value=json_extract(e.data,'$.values.account_key')
 LEFT JOIN source_post_url_evidence ue ON ue.uuid=r.url_evidence_uuid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row struct {
			models.AutomationEnrichmentRecord
			SnapshotUUID  string     `db:"snapshot_uuid"`
			Data          string     `db:"data"`
			Source        string     `db:"source_uuid"`
			Captured      string     `db:"captured_at"`
			PlatformUntil *time.Time `db:"platform_until"`
			AccountUntil  *time.Time `db:"account_until"`
			URLObservedAt *time.Time `db:"url_observed_at"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if scrape.CatalogSnapshotSHA([]byte(row.Data)) != row.SHA256 {
			return models.ErrAutomationSnapshotInvalid
		}
		values, err := automationTranslationValues(row.Data)
		if err != nil {
			return err
		}
		captured, err := time.Parse(time.RFC3339Nano, row.Captured)
		if err != nil {
			return err
		}
		if row.URLEvidenceUUID != nil && (*row.URLEvidenceUUID != scrape.RegistryImportUUID(row.SnapshotUUID, "automation-enrichment-url:v1", strconv.FormatInt(row.Ordinal, 10)) || row.URLObservedAt == nil || !row.URLObservedAt.Equal(captured)) {
			return models.ErrAutomationSnapshotInvalid
		}
		// Projections are family-specific. Original opaque fields remain in the
		// immutable source row instead of leaking into unrelated native meanings.
		if row.Table != "enrichment_jobs" && (row.HistoricalAttempts != nil || row.ServiceScope != "" || row.NotBefore != nil || row.StagedSHA256 != "" || row.PostReference != "" || row.TargetUUID != nil || row.URLUUID != nil || row.ReceiptUUID != nil || row.CaptureUUID != nil || row.AliasOrdinal != nil) ||
			row.Table != "enrichment_cooldowns" && (row.CooldownKind != "" || row.CooldownValue != "" || row.CooldownUntil != nil || row.CooldownReason != "") ||
			row.Table != "enrichment_seed_progress" && (row.SeedLastPostKey != nil || row.SeedComplete != nil || row.SeedCounts != nil) ||
			row.Table != "enrichment_source_progress" && (row.SourcePlatform != "" || row.SourceLastAttempt != nil) {
			return models.ErrAutomationSnapshotInvalid
		}
		reason := ""
		switch row.Table {
		case "enrichment_cooldowns":
			prepared, why := scrape.PrepareAutomationEnrichmentCooldown(values, captured)
			reason = why
			if prepared != nil && (row.Disposition != "cooldown" || row.Outcome != "mapped" || row.CooldownKind != prepared.Kind || row.CooldownValue != prepared.Value || row.CooldownReason != prepared.Reason || row.CooldownUntil == nil || !row.CooldownUntil.Equal(prepared.Until)) {
				return models.ErrAutomationSnapshotInvalid
			}
		case "enrichment_seed_progress":
			prepared, why := scrape.PrepareAutomationEnrichmentSeed(values)
			reason = why
			if prepared != nil && row.Outcome == "mapped" && (row.Disposition != "seed_progress" || row.SeedLastPostKey == nil || *row.SeedLastPostKey != prepared.LastPostKey || row.SeedComplete == nil || *row.SeedComplete != prepared.Complete) {
				return models.ErrAutomationSnapshotInvalid
			}
			if prepared != nil && row.Outcome == "mapped" {
				counts, err := archive.EncodeSourceJSON(prepared.Counts)
				if err != nil || row.SeedCounts == nil || !bytes.Equal(counts, *row.SeedCounts) {
					return models.ErrAutomationSnapshotInvalid
				}
			}
		case "enrichment_source_progress":
			prepared, why := scrape.PrepareAutomationEnrichmentSourceProgress(values, captured)
			reason = why
			if prepared != nil && (row.Disposition != "source_progress" || row.Outcome != "mapped" || row.SourcePlatform != prepared.Platform || row.SourceLastAttempt == nil || !row.SourceLastAttempt.Equal(prepared.LastAttempt)) {
				return models.ErrAutomationSnapshotInvalid
			}
		case "enrichment_jobs":
			prepared, why := scrape.PrepareAutomationEnrichmentJob(values, captured)
			reason = why
			if prepared != nil {
				if row.HistoricalAttempts == nil || *row.HistoricalAttempts != prepared.Attempts || row.ServiceScope != prepared.ServiceScope || row.StagedSHA256 != prepared.StagedSHA256 {
					return models.ErrAutomationSnapshotInvalid
				}
				ref, err := scrape.CatalogLocalPostReference(row.Source, prepared.CatalogID, prepared.PostKey)
				if err != nil || row.PostReference != ref.Value {
					return models.ErrAutomationSnapshotInvalid
				}
				if row.NotBefore != nil && (!validJobTime(*row.NotBefore) || row.NotBefore.Before(prepared.NotBefore)) {
					return models.ErrAutomationSnapshotInvalid
				}
				for _, until := range []*time.Time{row.PlatformUntil, row.AccountUntil} {
					if row.NotBefore != nil && until != nil && row.NotBefore.Before(*until) {
						return models.ErrAutomationSnapshotInvalid
					}
				}
				if row.Outcome == "mapped" && (row.PostUUID == nil || row.CatalogSnapshotUUID == nil || row.CollectionUUID == nil || row.NotBefore == nil) {
					return models.ErrAutomationSnapshotInvalid
				}
				if row.Outcome == "mapped" && row.Disposition != "preserved" && row.Disposition != prepared.Disposition {
					return models.ErrAutomationSnapshotInvalid
				}
				if prepared.StagedSHA256 != "" && (row.Outcome != "review" || row.TargetUUID != nil && row.CompletionUUID != nil) {
					return models.ErrAutomationSnapshotInvalid
				}
			}
		}
		if reason != "" && (row.Outcome != "review" || row.Disposition != "review" || row.Reason != reason || row.TargetUUID != nil || row.PostUUID != nil) {
			return models.ErrAutomationSnapshotInvalid
		}
	}
	return rows.Err()
}
