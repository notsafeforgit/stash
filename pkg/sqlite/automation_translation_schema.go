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

func validateAutomationTranslationSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"automation_translation_imports", "automation_translation_records", "automation_translation_import_guard",
		"automation_translation_record_immutable", "automation_translation_review", "automation_translation_target", "automation_translation_input"} {
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
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM automation_translation_imports i LEFT JOIN automation_snapshots s ON s.uuid=i.snapshot_uuid
 WHERE s.state IS NOT 'received' OR i.manifest_sha256 IS NOT s.manifest_sha256 OR i.policy!='automation-translations-v1'
 OR i.source_records!=(json_extract(CAST(s.manifest AS TEXT),'$.tables.translation_jobs.rows')+json_extract(CAST(s.manifest AS TEXT),'$.tables.translation_targets.rows'))
 OR i.processed_records!=(SELECT count(*) FROM automation_translation_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.mapped_records!=(SELECT count(*) FROM automation_translation_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='mapped')
 OR i.review_records!=(SELECT count(*) FROM automation_translation_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR i.last_ordinal!=(SELECT coalesce(max(r.ordinal),0) FROM automation_translation_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.processed_records!=(SELECT count(*) FROM automation_snapshot_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.source_table IN ('translation_jobs','translation_targets') AND r.ordinal<=i.last_ordinal)
 OR (i.state!='running' AND i.processed_records!=i.source_records) OR (i.state='mapped' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0))
 OR EXISTS(SELECT 1 FROM automation_translation_records r
 LEFT JOIN automation_translation_imports i ON i.snapshot_uuid=r.snapshot_uuid
 LEFT JOIN automation_snapshots s ON s.uuid=r.snapshot_uuid
 LEFT JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN automation_translation_records j ON j.snapshot_uuid=r.snapshot_uuid AND j.ordinal=r.job_ordinal
 LEFT JOIN automation_snapshot_records je ON je.snapshot_uuid=r.snapshot_uuid AND je.ordinal=r.job_ordinal
 LEFT JOIN translation_requests q ON q.uuid=r.request_uuid
 LEFT JOIN translation_cache c ON c.uuid=r.cache_uuid
 LEFT JOIN catalog_collection_mappings m ON m.source_uuid=s.source_uuid AND m.catalog_id=json_extract(e.data,'$.values.catalog_id')
 LEFT JOIN source_collection_revisions cr ON cr.collection_uuid=r.collection_uuid AND cr.revision=r.collection_revision
 LEFT JOIN source_post_identifiers p ON p.namespace=('legacy:catalog:'||s.source_uuid) AND p.value=r.post_reference
 LEFT JOIN translation_targets t ON t.uuid=r.target_uuid
 LEFT JOIN translation_target_history h ON h.target_uuid=r.target_uuid AND h.revision=r.target_revision
 LEFT JOIN source_translation_evidence x ON x.uuid=r.evidence_uuid
 WHERE i.snapshot_uuid IS NULL OR e.source_table NOT IN ('translation_jobs','translation_targets') OR e.source_table IS NULL
 OR (r.request_uuid IS NOT NULL AND q.uuid IS NULL)
 OR (e.source_table='translation_jobs' AND (r.job_ordinal IS NOT NULL OR r.post_uuid IS NOT NULL OR r.post_reference!='' OR r.collection_uuid IS NOT NULL OR r.target_uuid IS NOT NULL))
 OR (e.source_table='translation_targets' AND r.job_ordinal IS NULL AND (r.request_uuid IS NOT NULL OR r.cache_uuid IS NOT NULL))
 OR (r.cache_uuid IS NOT NULL AND (c.uuid IS NULL OR c.request_uuid IS NOT r.request_uuid))
 OR (r.job_ordinal IS NOT NULL AND (je.source_table IS NOT 'translation_jobs' OR j.ordinal IS NULL OR r.job_ordinal>=r.ordinal
  OR json_extract(je.data,'$.values.job_key') IS NOT json_extract(e.data,'$.values.job_key') OR r.request_uuid IS NOT j.request_uuid OR r.cache_uuid IS NOT j.cache_uuid))
 OR (r.collection_uuid IS NOT NULL AND (r.collection_uuid IS NOT m.collection_uuid OR m.import_uuid IS NOT s.registry_import_uuid OR r.collection_revision IS NOT 1 OR cr.origin IS NOT 'migration'))
 OR (r.post_uuid IS NOT NULL AND p.post_uuid IS NOT r.post_uuid)
 OR (r.target_uuid IS NOT NULL AND (t.uuid IS NULL OR h.target_uuid IS NULL OR t.request_uuid IS NOT r.request_uuid OR t.post_uuid IS NOT r.post_uuid
  OR t.collection_uuid IS NOT r.collection_uuid OR t.collection_revision IS NOT r.collection_revision OR t.field IS NOT json_extract(e.data,'$.values.field')))
 OR (r.disposition IN ('request','cache','english_original') AND e.source_table!='translation_jobs')
 OR (r.disposition IN ('held','completed','preserved') AND (e.source_table!='translation_targets' OR j.outcome IS NOT 'mapped'
  OR json_type(e.data,'$.values.applied') IS NOT 'integer' OR json_extract(e.data,'$.values.applied') NOT IN (0,1)))
 OR (r.disposition='held' AND (t.origin!='migration' OR r.target_revision!=1 OR h.state!='held' OR json_extract(e.data,'$.values.applied')!=0))
 OR (r.disposition='completed' AND (t.origin!='migration' OR r.target_revision!=2 OR h.state!='completed' OR h.cache_uuid IS NOT r.cache_uuid
  OR h.evidence_uuid IS NOT r.evidence_uuid OR json_extract(e.data,'$.values.applied')!=1))
 OR (r.evidence_uuid IS NOT NULL AND (x.uuid IS NULL OR x.origin!='migration' OR x.provenance!=('automation-translation:'||t.field)
  OR x.captured_at!='' OR x.post_uuid IS NOT r.post_uuid OR x.translation_uuid IS NOT c.translation_uuid
  OR x.collection_uuid IS NOT r.collection_uuid OR x.collection_revision IS NOT r.collection_revision
  OR json_extract(x.details,'$.policy') IS NOT i.policy OR json_extract(x.details,'$.snapshot_uuid') IS NOT r.snapshot_uuid
  OR json_extract(x.details,'$.source_ordinal') IS NOT r.ordinal OR json_extract(x.details,'$.target_uuid') IS NOT r.target_uuid
  OR json_extract(x.details,'$.cache_uuid') IS NOT r.cache_uuid OR json_extract(x.details,'$.request_uuid') IS NOT r.request_uuid)))
 OR EXISTS(SELECT 1 FROM translation_targets t JOIN source_translation_evidence e ON e.uuid=t.evidence_uuid
 WHERE e.origin='migration' AND NOT EXISTS(SELECT 1 FROM automation_translation_records r WHERE r.target_uuid=t.uuid
  AND r.target_revision=t.revision AND r.evidence_uuid=e.uuid AND r.disposition='completed'))`)
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid automation translation progress or reference scope")
	}
	if err := validateAutomationTranslationJobs(conn); err != nil {
		return err
	}
	return validateAutomationTranslationTargets(conn)
}

func automationTranslationValues(data string) (map[string]any, error) {
	object, err := archive.DecodeJSONObject([]byte(data), scrape.CatalogChunkLimit)
	if err != nil {
		return nil, err
	}
	values, ok := object["values"].(map[string]any)
	if !ok {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	return values, nil
}

func validateAutomationTranslationJobs(conn *sqlx.DB) error {
	rows, err := conn.Queryx(`SELECT e.data,s.captured_at,r.request_uuid,r.cache_uuid,r.disposition,r.outcome,r.reason,n.uuid AS native_cache_uuid
 FROM automation_translation_records r JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 JOIN automation_snapshots s ON s.uuid=r.snapshot_uuid LEFT JOIN translation_cache n ON n.request_uuid=r.request_uuid WHERE e.source_table='translation_jobs'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row struct {
			models.AutomationTranslationRecord
			Data        string  `db:"data"`
			Captured    string  `db:"captured_at"`
			NativeCache *string `db:"native_cache_uuid"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		values, err := automationTranslationValues(row.Data)
		if err != nil {
			return err
		}
		captured, err := time.Parse(time.RFC3339Nano, row.Captured)
		if err != nil {
			return err
		}
		prepared, reason := scrape.PrepareAutomationTranslationJob(values, captured)
		if reason != "" {
			if row.Outcome != "review" || row.Reason != reason || row.RequestUUID != nil || row.CacheUUID != nil {
				return models.ErrAutomationSnapshotInvalid
			}
			continue
		}
		request, err := archive.PrepareTranslationRequest(prepared.Request)
		if err != nil || row.RequestUUID == nil || *row.RequestUUID != request.UUID {
			return models.ErrAutomationSnapshotInvalid
		}
		if row.Outcome == "review" {
			if row.Reason != "native_cache_conflict" || prepared.Cache == nil || row.CacheUUID != nil || row.NativeCache == nil {
				return models.ErrAutomationSnapshotInvalid
			}
			cache, _, err := archive.PrepareTranslationCache(request, *prepared.Cache)
			if err != nil || cache.UUID == *row.NativeCache {
				return models.ErrAutomationSnapshotInvalid
			}
			continue
		}
		if row.Disposition != prepared.Disposition || (prepared.Cache == nil) != (row.CacheUUID == nil) {
			return models.ErrAutomationSnapshotInvalid
		}
		if prepared.Cache != nil {
			cache, _, err := archive.PrepareTranslationCache(request, *prepared.Cache)
			if err != nil || cache.UUID != *row.CacheUUID {
				return models.ErrAutomationSnapshotInvalid
			}
		}
	}
	return rows.Err()
}

func validateAutomationTranslationTargets(conn *sqlx.DB) error {
	rows, err := conn.Queryx(`SELECT r.post_reference,r.disposition,e.data,s.source_uuid,s.captured_at,je.data AS job_data,h.priority,h.not_before
 FROM automation_translation_records r JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 JOIN automation_snapshots s ON s.uuid=r.snapshot_uuid
 JOIN automation_snapshot_records je ON je.snapshot_uuid=r.snapshot_uuid AND je.ordinal=r.job_ordinal
 LEFT JOIN translation_target_history h ON h.target_uuid=r.target_uuid AND h.revision=r.target_revision
 WHERE r.post_reference!=''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row struct {
			Reference   string     `db:"post_reference"`
			Disposition string     `db:"disposition"`
			Data        string     `db:"data"`
			Source      string     `db:"source_uuid"`
			Captured    string     `db:"captured_at"`
			Job         string     `db:"job_data"`
			Priority    *int       `db:"priority"`
			NotBefore   *time.Time `db:"not_before"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		values, err := automationTranslationValues(row.Data)
		if err != nil {
			return err
		}
		input, reason := scrape.PrepareAutomationTranslationTarget(values)
		if reason != "" {
			return models.ErrAutomationSnapshotInvalid
		}
		ref, err := scrape.CatalogLocalPostReference(row.Source, input.CatalogID, input.PostKey)
		if err != nil || ref.Value != row.Reference {
			return models.ErrAutomationSnapshotInvalid
		}
		if row.Disposition == "held" || row.Disposition == "completed" {
			values, err := automationTranslationValues(row.Job)
			if err != nil {
				return err
			}
			captured, err := time.Parse(time.RFC3339Nano, row.Captured)
			if err != nil {
				return err
			}
			job, reason := scrape.PrepareAutomationTranslationJob(values, captured)
			if reason != "" || row.Priority == nil || row.NotBefore == nil || *row.Priority != job.Priority || !row.NotBefore.Equal(job.NotBefore) {
				return models.ErrAutomationSnapshotInvalid
			}
		}
	}
	return rows.Err()
}
