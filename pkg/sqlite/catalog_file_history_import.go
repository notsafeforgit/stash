package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type CatalogFileHistoryImportStore struct{}

const catalogFileHistoryPolicy = "catalog-file-history-v1"
const catalogFileHistoryTables = "('metadata_edits','file_events','dedupe_events')"
const catalogFileHistoryColumns = `r.ordinal,e.source_table,e.source_key,e.data_sha256,r.history_uuid,r.outcome,r.reason`
const catalogFileHistoryJoins = ` FROM catalog_file_history_records r JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal`

func (s *CatalogFileHistoryImportStore) Find(ctx context.Context, id string) (*models.CatalogFileHistoryImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.CatalogFileHistoryImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM catalog_file_history_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

func catalogHistoryLocation(ctx context.Context, w *catalogMediaWork, path string) (*models.SourceFileHistoryLocation, *models.SourceFileObservation, error) {
	if !archive.ValidRootRelativePath(path, false) {
		return nil, nil, models.ErrSourceFileHistoryInvalid
	}
	location := &models.SourceFileHistoryLocation{RelativePath: path}
	mapped, err := w.mappedRow(ctx, "files", path)
	if err != nil || mapped == nil || mapped.ObservationUUID == nil {
		return location, nil, err
	}
	observation, err := (&SourceFileStore{}).Observation(ctx, *mapped.ObservationUUID)
	if err != nil {
		return nil, nil, err
	}
	if observation == nil {
		return nil, nil, models.ErrSourcePayloadCorrupt
	}
	location.ObservationUUID, location.RelativePath, location.ArchivePath = &observation.UUID, observation.RelativePath, observation.ArchivePath
	return location, observation, nil
}

func catalogHistoryRecord(ctx context.Context, w *catalogMediaWork, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord) (*models.CatalogFileHistoryRecord, error) {
	ret := &models.CatalogFileHistoryRecord{Ordinal: row.Ordinal, Outcome: "review", Reason: "invalid_history_values"}
	v := record.Values
	details, err := w.details(row)
	if err != nil {
		return nil, err
	}
	input := models.SourceFileHistory{UUID: scrape.RegistryImportUUID(w.snapshot.UUID, "catalog-file-history:v1", strconv.FormatInt(row.Ordinal, 10)),
		CollectionUUID: w.snapshot.CollectionUUID, CollectionRevision: w.progress.CollectionRevision, RootUUID: w.progress.RootUUID, RootRevision: w.progress.RootRevision,
		ReferenceNamespace: "legacy:catalog:" + w.snapshot.SourceUUID + ":" + w.snapshot.CatalogID, Origin: "migration", ObservedAt: w.stamp, Details: details}
	key, stamp := "event_id", "created_at"
	switch record.Table {
	case "metadata_edits":
		key = "edit_id"
	case "file_events":
		stamp = "observed_at"
	}
	var ok bool
	input.ReferenceValue, ok = v[key].(string)
	if !ok {
		return ret, nil
	}
	input.SourceTime, ok = v[stamp].(string)
	if !ok {
		return ret, nil
	}
	review := ""
	if record.Table == "dedupe_events" {
		input.Kind = "deduplication"
		asset, assetOK := v["asset_id"].(string)
		pathsJSON, pathsOK := v["paths_json"].(string)
		stage, stageOK := v["stage"].(string)
		survivor, survivorOK := catalogMediaText(v["survivor_relpath"])
		if !assetOK || !pathsOK || !stageOK || !survivorOK {
			return ret, nil
		}
		mapped, err := w.mappedRow(ctx, "assets", asset)
		if err != nil {
			return nil, err
		}
		if mapped == nil || mapped.ClaimUUID == nil {
			ret.Reason = "content_claim_requires_review"
			return ret, nil
		}
		object, err := archive.DecodeJSONObject([]byte(`{"paths":`+pathsJSON+`}`), archive.MaxFileHistoryEditBytes)
		if err != nil {
			return ret, nil
		}
		paths, ok := object["paths"].([]any)
		if !ok || len(paths) < 2 || len(paths) > 1024 {
			return ret, nil
		}
		input.Deduplication = &models.SourceFileDeduplication{ContentClaimUUID: *mapped.ClaimUUID, Stage: stage, SurvivorPath: survivor}
		for i, value := range paths {
			path, ok := value.(string)
			if !ok {
				return ret, nil
			}
			location, observation, err := catalogHistoryLocation(ctx, w, path)
			if errors.Is(err, models.ErrSourceFileHistoryInvalid) {
				return ret, nil
			}
			if err != nil {
				return nil, err
			}
			if observation == nil || observation.ContentClaimUUID == nil || *observation.ContentClaimUUID != *mapped.ClaimUUID {
				// Retain the declared historical path without attaching a later
				// observation of different content to this old deduplication.
				location.ObservationUUID, location.ArchivePath, location.RelativePath = nil, nil, path
				review = "deduplication_member_requires_review"
			}
			location.Position = i
			input.Locations = append(input.Locations, *location)
		}
	} else {
		path, ok := v["relpath"].(string)
		if !ok {
			return ret, nil
		}
		location, observation, err := catalogHistoryLocation(ctx, w, path)
		if errors.Is(err, models.ErrSourceFileHistoryInvalid) {
			return ret, nil
		}
		if err != nil {
			return nil, err
		}
		if observation == nil {
			ret.Reason = "file_observation_requires_review"
			return ret, nil
		}
		input.Locations = []models.SourceFileHistoryLocation{*location}
		switch record.Table {
		case "metadata_edits":
			input.Kind = "metadata_edit"
			fields, ok := v["fields_json"].(string)
			if !ok {
				return ret, nil
			}
			input.Edits, err = archive.CatalogFileEdits([]byte(fields))
			if err != nil {
				return ret, nil
			}
			for _, edit := range input.Edits {
				if edit.Mode == "unmapped" {
					review = "metadata_fields_require_review"
				}
			}
		case "file_events":
			input.Kind = "state_change"
			oldState, oldOK := v["old_state"].(string)
			newState, newOK := v["new_state"].(string)
			reason, reasonOK := v["reason"].(string)
			if !oldOK || !newOK || !reasonOK {
				return ret, nil
			}
			input.StateChange = &models.SourceFileStateChange{OldState: oldState, NewState: newState, Reason: reason}
		default:
			return nil, models.ErrCatalogSnapshotInvalid
		}
	}
	history, err := (&SourceFileHistoryStore{}).Record(ctx, input)
	if errors.Is(err, models.ErrSourceFileHistoryInvalid) {
		return ret, nil
	}
	if err != nil {
		return nil, err
	}
	ret.HistoryUUID, ret.Reason = &history.UUID, review
	if review == "" {
		ret.Outcome = "mapped"
	}
	return ret, nil
}

func (s *CatalogFileHistoryImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.CatalogFileHistoryImport, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || !archive.ValidSHA256(expected) || after < 0 || !validJobTime(now) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	prior, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if (prior == nil && after != 0) || (prior != nil && (prior.LastOrdinal != after || prior.ManifestSHA256 != expected || prior.Policy != catalogFileHistoryPolicy)) {
		return nil, models.ErrCatalogSnapshotConflict
	}
	if prior != nil && prior.State != "running" {
		return prior, nil
	}
	w, err := loadCatalogMediaWork(ctx, id, expected)
	if err != nil {
		return nil, err
	}
	w.progress, err = (&CatalogMediaImportStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if w.progress == nil || w.progress.State == "running" || w.progress.ManifestSHA256 != expected {
		return nil, models.ErrCatalogSnapshotConflict
	}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		total := w.manifest.Tables["metadata_edits"].Rows + w.manifest.Tables["file_events"].Rows + w.manifest.Tables["dedupe_events"].Rows
		_, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_file_history_imports(snapshot_uuid,manifest_sha256,collection_uuid,collection_revision,
 root_uuid,root_revision,policy,state,source_records,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'running',?,?,?)`,
			id, expected, w.snapshot.CollectionUUID, w.progress.CollectionRevision, w.progress.RootUUID, w.progress.RootRevision, catalogFileHistoryPolicy, total, stamp, stamp)
		if err != nil {
			return nil, err
		}
		prior, err = s.Find(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	for count := 0; count < 50 && w.bytes < 16<<20; count++ {
		var row catalogEvidenceRow
		err := dbWrapper.Get(ctx, &row, "SELECT ordinal,data,data_sha256 FROM catalog_snapshot_records WHERE snapshot_uuid=? AND source_table IN "+catalogFileHistoryTables+" AND ordinal>? ORDER BY ordinal LIMIT 1", id, prior.LastOrdinal)
		if errors.Is(err, sql.ErrNoRows) {
			if prior.ProcessedRecords != prior.TotalRecords {
				return nil, models.ErrCatalogSnapshotInvalid
			}
			prior.State = "mapped"
			if prior.ReviewRecords != 0 {
				prior.State = "review"
			}
			break
		}
		if err != nil {
			return nil, err
		}
		record, err := w.decode(row)
		if err != nil {
			return nil, err
		}
		result, err := catalogHistoryRecord(ctx, w, row, record)
		if err != nil {
			return nil, err
		}
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO catalog_file_history_records VALUES(?,?,?,?,?)", id, row.Ordinal, result.HistoryUUID, result.Outcome, result.Reason); err != nil {
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
	_, err = dbWrapper.Exec(ctx, `UPDATE catalog_file_history_imports SET state=?,last_ordinal=?,processed_records=?,mapped_records=?,review_records=?,updated_at=? WHERE snapshot_uuid=?`,
		prior.State, prior.LastOrdinal, prior.ProcessedRecords, prior.MappedRecords, prior.ReviewRecords, stamp, id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, id)
	*complete = err == nil
	return ret, err
}

func (s *CatalogFileHistoryImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.CatalogFileHistoryRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	columns := strings.Replace(catalogFileHistoryColumns, "e.source_key", `CASE WHEN length(CAST(e.source_key AS BLOB))<=8192 THEN e.source_key ELSE '' END AS source_key,length(CAST(e.source_key AS BLOB))>8192 AS key_omitted`, 1)
	ret := []models.CatalogFileHistoryRecord{}
	err := dbWrapper.Select(ctx, &ret, "SELECT "+columns+catalogFileHistoryJoins+" WHERE r.snapshot_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?", id, after, limit)
	return ret, err
}

func (s *CatalogFileHistoryImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.CatalogFileHistoryRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	var row struct {
		models.CatalogFileHistoryRecord
		Data string `db:"data"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+catalogFileHistoryColumns+",e.data"+catalogFileHistoryJoins+" WHERE r.snapshot_uuid=? AND r.ordinal=?", id, ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	object, err := archive.DecodeJSONObject([]byte(row.Data), scrape.CatalogChunkLimit)
	if err != nil || scrape.CatalogSnapshotSHA([]byte(row.Data)) != row.SHA256 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	values, err := archive.EncodeSourceJSON(object["values"])
	return &models.CatalogFileHistoryRecordDetails{CatalogFileHistoryRecord: row.CatalogFileHistoryRecord, SourceValues: values}, err
}
