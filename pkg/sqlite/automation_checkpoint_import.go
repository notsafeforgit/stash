package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type AutomationCheckpointImportStore struct{}

const automationStagedInput = `source_table='enrichment_jobs' AND coalesce(json_type(data,'$.values.staged_json'),'null')!='null'`

func (s *AutomationCheckpointImportStore) Find(ctx context.Context, id string) (*models.AutomationCheckpointImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	ret := &models.AutomationCheckpointImport{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM automation_checkpoint_imports WHERE snapshot_uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func prepareAutomationCheckpoint(row catalogEvidenceRow) (*models.AutomationCheckpointRecord, json.RawMessage, error) {
	if scrape.CatalogSnapshotSHA([]byte(row.Data)) != row.SHA256 {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	tree, err := archive.DecodeJSONObject([]byte(row.Data), scrape.CatalogChunkLimit)
	if err != nil || tree["table"] != "enrichment_jobs" {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	values, ok := tree["values"].(map[string]any)
	if !ok || values["staged_json"] == nil {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	ret := &models.AutomationCheckpointRecord{Ordinal: row.Ordinal, SourceSHA256: row.SHA256, Outcome: "review", Reason: "invalid_legacy_checkpoint_type"}
	raw, ok := values["staged_json"].(string)
	if !ok {
		return ret, nil, nil
	}
	stagedSHA := scrape.CatalogSnapshotSHA([]byte(raw))
	ret.StagedSHA256 = &stagedSHA
	if values["version"] != json.Number("1") {
		ret.Reason = "unsupported_legacy_enrichment_version"
		return ret, nil, nil
	}
	converted, body, reason := scrape.ConvertEnrichmentStaging([]byte(raw))
	if reason != "" {
		ret.Reason = reason
		return ret, nil, nil
	}
	digest := scrape.CatalogSnapshotSHA(body)
	ret.BodySHA256, ret.Outcome, ret.Reason = &digest, "mapped", ""
	ret.RecordCount, ret.PendingCount, ret.UnresolvedCount = len(converted.Records), len(converted.Pending), len(converted.Unresolved)
	return ret, body, nil
}

func (s *AutomationCheckpointImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.AutomationCheckpointImport, error) {
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
	if (prior == nil && after != 0) || (prior != nil && (prior.ManifestSHA256 != expected || prior.LastOrdinal != after || prior.Policy != scrape.EnrichmentStagingPolicy)) {
		return nil, models.ErrAutomationSnapshotConflict
	}
	if prior != nil && prior.State != "running" {
		return prior, nil
	}
	parent, err := (&AutomationEnrichmentImportStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if parent == nil || parent.State == "running" || parent.ManifestSHA256 != expected {
		return nil, models.ErrAutomationSnapshotConflict
	}
	updated, err := time.Parse(time.RFC3339Nano, parent.UpdatedAt)
	if err != nil || now.Before(updated) {
		return nil, models.ErrAutomationSnapshotConflict
	}
	if prior != nil {
		updated, err = time.Parse(time.RFC3339Nano, prior.UpdatedAt)
		if err != nil || now.Before(updated) {
			return nil, models.ErrAutomationSnapshotConflict
		}
	}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		var total int64
		if err := dbWrapper.Get(ctx, &total, "SELECT count(*) FROM automation_snapshot_records INDEXED BY automation_enrichment_staged_input WHERE snapshot_uuid=? AND "+automationStagedInput, id); err != nil {
			return nil, err
		}
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO automation_checkpoint_imports(snapshot_uuid,manifest_sha256,policy,state,source_records,created_at,updated_at)
 VALUES(?,?,?,'running',?,?,?)`, id, expected, scrape.EnrichmentStagingPolicy, total, stamp, stamp); err != nil {
			return nil, err
		}
		prior, err = s.Find(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	bytesRead := 0
	for count := 0; count < 100; count++ {
		var row catalogEvidenceRow
		err := dbWrapper.Get(ctx, &row, "SELECT ordinal,data,data_sha256 FROM automation_snapshot_records INDEXED BY automation_enrichment_staged_input WHERE snapshot_uuid=? AND ordinal>? AND "+automationStagedInput+" ORDER BY ordinal LIMIT 1", id, prior.LastOrdinal)
		if errors.Is(err, sql.ErrNoRows) {
			if prior.TotalRecords != prior.ProcessedRecords {
				return nil, models.ErrSourcePayloadCorrupt
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
		if count > 0 && bytesRead+len(row.Data) > scrape.CatalogChunkLimit {
			break
		}
		bytesRead += len(row.Data)
		result, body, err := prepareAutomationCheckpoint(row)
		if err != nil {
			return nil, err
		}
		if body != nil {
			if _, err := dbWrapper.Exec(ctx, "INSERT INTO automation_checkpoint_bodies(hash,body) VALUES(?,?) ON CONFLICT(hash) DO NOTHING", *result.BodySHA256, string(body)); err != nil {
				return nil, err
			}
			var saved []byte
			if err := dbWrapper.Get(ctx, &saved, "SELECT body FROM automation_checkpoint_bodies WHERE hash=?", *result.BodySHA256); err != nil {
				return nil, err
			}
			if !bytes.Equal(saved, body) {
				return nil, models.ErrSourcePayloadCorrupt
			}
		}
		_, err = dbWrapper.Exec(ctx, `INSERT INTO automation_checkpoint_records(snapshot_uuid,ordinal,source_sha256,staged_sha256,body_sha256,record_count,pending_count,unresolved_count,outcome,reason)
 VALUES(?,?,?,?,?,?,?,?,?,?)`, id, row.Ordinal, result.SourceSHA256, result.StagedSHA256, result.BodySHA256, result.RecordCount, result.PendingCount, result.UnresolvedCount, result.Outcome, result.Reason)
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
	_, err = dbWrapper.Exec(ctx, `UPDATE automation_checkpoint_imports SET state=?,last_ordinal=?,processed_records=?,mapped_records=?,review_records=?,updated_at=? WHERE snapshot_uuid=?`,
		prior.State, prior.LastOrdinal, prior.ProcessedRecords, prior.MappedRecords, prior.ReviewRecords, stamp, id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, id)
	*complete = err == nil
	return ret, err
}

const automationCheckpointColumns = `r.ordinal,r.source_sha256,r.staged_sha256,r.body_sha256,r.record_count,r.pending_count,r.unresolved_count,r.outcome,r.reason`

func (s *AutomationCheckpointImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.AutomationCheckpointRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	ret := []models.AutomationCheckpointRecord{}
	err := dbWrapper.Select(ctx, &ret, "SELECT "+automationCheckpointColumns+" FROM automation_checkpoint_records r WHERE snapshot_uuid=? AND ordinal>? ORDER BY ordinal LIMIT ?", id, after, limit)
	return ret, err
}

func (s *AutomationCheckpointImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.AutomationCheckpointRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	var row struct {
		models.AutomationCheckpointRecord
		Data string  `db:"data"`
		Body *string `db:"body"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+automationCheckpointColumns+`,e.data,b.body FROM automation_checkpoint_records r
 JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN automation_checkpoint_bodies b ON b.hash=r.body_sha256 WHERE r.snapshot_uuid=? AND r.ordinal=?`, id, ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	expected, body, err := prepareAutomationCheckpoint(catalogEvidenceRow{Ordinal: ordinal, Data: row.Data, SHA256: row.SourceSHA256})
	if err != nil || !sameAutomationCheckpoint(*expected, row.AutomationCheckpointRecord) || (row.Body == nil) != (body == nil) || (row.Body != nil && *row.Body != string(body)) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	tree, err := archive.DecodeJSONObject([]byte(row.Data), scrape.CatalogChunkLimit)
	if err != nil {
		return nil, err
	}
	values, err := archive.EncodeSourceJSON(tree["values"])
	return &models.AutomationCheckpointRecordDetails{AutomationCheckpointRecord: row.AutomationCheckpointRecord, SourceValues: values, Body: body}, err
}

func sameAutomationCheckpoint(a, b models.AutomationCheckpointRecord) bool {
	return reflect.DeepEqual(a, b)
}
