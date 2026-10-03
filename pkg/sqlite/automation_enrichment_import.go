package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type AutomationEnrichmentImportStore struct{}
type automationEnrichmentWork struct{ automationImportWork }

const automationEnrichmentColumns = `r.ordinal,e.source_table,e.source_key,e.data_sha256,r.post_uuid,r.post_reference,r.catalog_snapshot_uuid,r.collection_uuid,r.collection_revision,r.url_uuid,r.url_evidence_uuid,r.target_uuid,r.target_revision,r.receipt_uuid,r.capture_uuid,r.completion_uuid,r.alias_ordinal,r.historical_attempts,r.service_scope,r.not_before,r.staged_sha256,r.cooldown_kind,r.cooldown_value,r.cooldown_until,r.cooldown_reason,r.seed_last_post_key,r.seed_complete,CAST(r.seed_counts AS BLOB) AS seed_counts,r.source_platform,r.source_last_attempt,r.disposition,r.outcome,r.reason`
const automationEnrichmentJoins = ` FROM automation_enrichment_records r JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal`

func (s *AutomationEnrichmentImportStore) Find(ctx context.Context, id string) (*models.AutomationEnrichmentImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	ret := &models.AutomationEnrichmentImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM automation_enrichment_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

func (s *AutomationEnrichmentImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.AutomationEnrichmentImport, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || !archive.ValidSHA256(expected) || after < 0 || !validJobTime(now) {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	prior, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if (prior == nil && after != 0) || (prior != nil && (prior.LastOrdinal != after || prior.ManifestSHA256 != expected || prior.Policy != scrape.AutomationEnrichmentPolicy)) {
		return nil, models.ErrAutomationSnapshotConflict
	}
	if prior != nil && prior.State != "running" {
		return prior, nil
	}
	snapshot, err := (&AutomationSnapshotStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || snapshot.State != "received" || snapshot.ManifestSHA256 != expected {
		return nil, models.ErrAutomationSnapshotConflict
	}
	var body []byte
	if err := dbWrapper.Get(ctx, &body, "SELECT manifest FROM automation_snapshots WHERE uuid=?", id); err != nil {
		return nil, err
	}
	manifest, err := scrape.PrepareAutomationSnapshot(body, expected)
	if err != nil {
		return nil, err
	}
	captured, _ := time.Parse(time.RFC3339Nano, manifest.CapturedAt)
	updated, _ := time.Parse(time.RFC3339Nano, snapshot.UpdatedAt)
	if prior != nil {
		updated, _ = time.Parse(time.RFC3339Nano, prior.UpdatedAt)
	}
	if now.Before(updated) || now.Before(captured) {
		return nil, models.ErrAutomationSnapshotConflict
	}
	w := &automationEnrichmentWork{automationImportWork{snapshot, manifest, captured}}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		total := manifest.Tables["enrichment_cooldowns"].Rows + manifest.Tables["enrichment_jobs"].Rows + manifest.Tables["enrichment_seed_progress"].Rows + manifest.Tables["enrichment_source_progress"].Rows
		_, err := dbWrapper.Exec(ctx, `INSERT INTO automation_enrichment_imports(snapshot_uuid,manifest_sha256,policy,state,source_records,created_at,updated_at)
 VALUES(?,?,?,'running',?,?,?)`, id, expected, scrape.AutomationEnrichmentPolicy, total, stamp, stamp)
		if err != nil {
			return nil, err
		}
		prior, err = s.Find(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	var bytesRead int
	for count := 0; count < 200; count++ {
		var row catalogEvidenceRow
		err := dbWrapper.Get(ctx, &row, `SELECT ordinal,data,data_sha256 FROM automation_snapshot_records INDEXED BY automation_enrichment_input
 WHERE snapshot_uuid=? AND ordinal>? AND source_table IN ('enrichment_cooldowns','enrichment_jobs','enrichment_seed_progress','enrichment_source_progress') ORDER BY ordinal LIMIT 1`, id, prior.LastOrdinal)
		if errors.Is(err, sql.ErrNoRows) {
			if prior.ProcessedRecords != prior.TotalRecords {
				return nil, models.ErrAutomationSnapshotInvalid
			}
			prior.State = "mapped"
			if prior.ReviewRecords > 0 {
				prior.State = "review"
			}
			break
		}
		if err != nil {
			return nil, err
		}
		if bytesRead+len(row.Data) > scrape.CatalogChunkLimit && count > 0 {
			break
		}
		bytesRead += len(row.Data)
		record, err := w.decode(row)
		if err != nil {
			return nil, err
		}
		result, err := w.record(ctx, row, record, now)
		if err != nil {
			return nil, err
		}
		var seedCounts any
		if result.SeedCounts != nil {
			seedCounts = string(*result.SeedCounts)
		}
		_, err = dbWrapper.Exec(ctx, `INSERT INTO automation_enrichment_records(snapshot_uuid,ordinal,post_uuid,post_reference,catalog_snapshot_uuid,collection_uuid,collection_revision,url_uuid,url_evidence_uuid,target_uuid,target_revision,receipt_uuid,capture_uuid,completion_uuid,alias_ordinal,historical_attempts,service_scope,not_before,staged_sha256,cooldown_kind,cooldown_value,cooldown_until,cooldown_reason,seed_last_post_key,seed_complete,seed_counts,source_platform,source_last_attempt,disposition,outcome,reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, row.Ordinal, result.PostUUID, result.PostReference, result.CatalogSnapshotUUID, result.CollectionUUID, result.CollectionRevision, result.URLUUID, result.URLEvidenceUUID, result.TargetUUID, result.TargetRevision, result.ReceiptUUID, result.CaptureUUID, result.CompletionUUID, result.AliasOrdinal, result.HistoricalAttempts, result.ServiceScope, result.NotBefore, result.StagedSHA256, result.CooldownKind, result.CooldownValue, result.CooldownUntil, result.CooldownReason, result.SeedLastPostKey, result.SeedComplete, seedCounts, result.SourcePlatform, result.SourceLastAttempt, result.Disposition, result.Outcome, result.Reason)
		if err != nil {
			return nil, err
		}

		prior.ProcessedRecords++
		prior.LastOrdinal = row.Ordinal
		if result.Outcome == "mapped" {
			prior.MappedRecords++
		} else {
			prior.ReviewRecords++
		}
	}
	_, err = dbWrapper.Exec(ctx, `UPDATE automation_enrichment_imports SET state=?,last_ordinal=?,processed_records=?,mapped_records=?,review_records=?,updated_at=? WHERE snapshot_uuid=?`,
		prior.State, prior.LastOrdinal, prior.ProcessedRecords, prior.MappedRecords, prior.ReviewRecords, stamp, id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, id)
	*complete = err == nil
	return ret, err
}

func (s *AutomationEnrichmentImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.AutomationEnrichmentRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	columns := strings.Replace(automationEnrichmentColumns, "e.source_key", `CASE WHEN length(CAST(e.source_key AS BLOB))<=8192 THEN e.source_key ELSE '' END AS source_key,length(CAST(e.source_key AS BLOB))>8192 AS key_omitted`, 1)
	ret := []models.AutomationEnrichmentRecord{}
	err := dbWrapper.Select(ctx, &ret, "SELECT "+columns+automationEnrichmentJoins+" WHERE r.snapshot_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?", id, after, limit)
	return ret, err
}

func (s *AutomationEnrichmentImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.AutomationEnrichmentRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	var row struct {
		models.AutomationEnrichmentRecord
		Data string `db:"data"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+automationEnrichmentColumns+",e.data"+automationEnrichmentJoins+" WHERE r.snapshot_uuid=? AND r.ordinal=?", id, ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	object, err := archive.DecodeJSONObject([]byte(row.Data), scrape.CatalogChunkLimit)
	if err != nil || scrape.CatalogSnapshotSHA([]byte(row.Data)) != row.SHA256 {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	values, err := archive.EncodeSourceJSON(object["values"])
	return &models.AutomationEnrichmentRecordDetails{AutomationEnrichmentRecord: row.AutomationEnrichmentRecord, SourceValues: values}, err
}
