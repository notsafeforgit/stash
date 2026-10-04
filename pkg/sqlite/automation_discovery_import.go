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

type AutomationDiscoveryImportStore struct{}
type automationDiscoveryWork struct{ automationImportWork }

const automationDiscoveryStoredColumns = `ordinal,account_uuid,account_ordinal,target_ordinal,enrichment_ordinal,post_uuid,post_reference,catalog_snapshot_uuid,collection_uuid,collection_revision,phase,service_scope,profile_url,not_before,historical_attempts,historical_pages,cursor_sha256,staged_sha256,staged_kind,evidence_sha256,candidate_url,candidate_basis,payload_sha256,maintenance_kind,maintenance_time,watermark_ns,disposition,outcome,reason`
const automationDiscoveryFamilies = `source_table IN ('discovery_accounts','discovery_targets','discovery_candidates','maintenance')`

var automationDiscoveryColumns = "r." + strings.ReplaceAll(automationDiscoveryStoredColumns, ",", ",r.") + ",e.source_table,e.source_key,e.data_sha256"

const automationDiscoveryJoins = ` FROM automation_discovery_records r JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal`

func (s *AutomationDiscoveryImportStore) Find(ctx context.Context, id string) (*models.AutomationDiscoveryImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	ret := &models.AutomationDiscoveryImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM automation_discovery_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

func (s *AutomationDiscoveryImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.AutomationDiscoveryImport, error) {
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
	if (prior == nil && after != 0) || (prior != nil && (prior.LastOrdinal != after || prior.ManifestSHA256 != expected || prior.Policy != scrape.AutomationDiscoveryPolicy)) {
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
	enrichment, err := (&AutomationEnrichmentImportStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if enrichment == nil || enrichment.State == "running" || enrichment.ManifestSHA256 != expected {
		return nil, models.ErrAutomationSnapshotConflict
	}
	enrichmentTime, err := time.Parse(time.RFC3339Nano, enrichment.UpdatedAt)
	if err != nil || now.Before(enrichmentTime) {
		return nil, models.ErrAutomationSnapshotConflict
	}
	w := &automationDiscoveryWork{automationImportWork{snapshot, manifest, captured}}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		total := manifest.Tables["discovery_accounts"].Rows + manifest.Tables["discovery_targets"].Rows + manifest.Tables["discovery_candidates"].Rows + manifest.Tables["maintenance"].Rows
		_, err := dbWrapper.Exec(ctx, `INSERT INTO automation_discovery_imports(snapshot_uuid,manifest_sha256,policy,state,source_records,created_at,updated_at)
 VALUES(?,?,?,'running',?,?,?)`, id, expected, scrape.AutomationDiscoveryPolicy, total, stamp, stamp)
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
		err := dbWrapper.Get(ctx, &row, "SELECT ordinal,data,data_sha256 FROM automation_snapshot_records INDEXED BY automation_discovery_input WHERE snapshot_uuid=? AND ordinal>? AND "+automationDiscoveryFamilies+" ORDER BY ordinal LIMIT 1", id, prior.LastOrdinal)
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
		result, err := w.record(ctx, row, record)
		if err != nil {
			return nil, err
		}
		_, err = dbWrapper.NamedExec(ctx, "INSERT INTO automation_discovery_records(snapshot_uuid,"+automationDiscoveryStoredColumns+") VALUES(:snapshot_uuid,:"+strings.ReplaceAll(automationDiscoveryStoredColumns, ",", ",:")+")", struct {
			SnapshotUUID string `db:"snapshot_uuid"`
			models.AutomationDiscoveryRecord
		}{id, *result})
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
	_, err = dbWrapper.Exec(ctx, `UPDATE automation_discovery_imports SET state=?,last_ordinal=?,processed_records=?,mapped_records=?,review_records=?,updated_at=? WHERE snapshot_uuid=?`,
		prior.State, prior.LastOrdinal, prior.ProcessedRecords, prior.MappedRecords, prior.ReviewRecords, stamp, id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, id)
	*complete = err == nil
	return ret, err
}

func (s *AutomationDiscoveryImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.AutomationDiscoveryRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	columns := strings.Replace(automationDiscoveryColumns, "e.source_key", `CASE WHEN length(CAST(e.source_key AS BLOB))<=8192 THEN e.source_key ELSE '' END AS source_key,length(CAST(e.source_key AS BLOB))>8192 AS key_omitted`, 1)
	ret := []models.AutomationDiscoveryRecord{}
	err := dbWrapper.Select(ctx, &ret, "SELECT "+columns+automationDiscoveryJoins+" WHERE r.snapshot_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?", id, after, limit)
	return ret, err
}

func (s *AutomationDiscoveryImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.AutomationDiscoveryRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	var row struct {
		models.AutomationDiscoveryRecord
		Data string `db:"data"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+automationDiscoveryColumns+",e.data"+automationDiscoveryJoins+" WHERE r.snapshot_uuid=? AND r.ordinal=?", id, ordinal)
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
	return &models.AutomationDiscoveryRecordDetails{AutomationDiscoveryRecord: row.AutomationDiscoveryRecord, SourceValues: values}, err
}
