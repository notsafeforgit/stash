package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type CatalogMediaImportStore struct{}

const catalogMediaPolicy = "catalog-media-v1"
const catalogMediaColumns = `r.ordinal,e.source_table,e.source_key,e.data_sha256,r.claim_uuid,r.observation_uuid,r.match_uuid,r.post_uuid,r.post_file_uuid,r.media_evidence_uuid,r.outcome,r.reason`
const catalogMediaJoins = ` FROM catalog_media_records r JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal`

func (s *CatalogMediaImportStore) Find(ctx context.Context, id string) (*models.CatalogMediaImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.CatalogMediaImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM catalog_media_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

type catalogMediaWork struct {
	catalogRelationsWork
	progress *models.CatalogMediaImport
	stamp    time.Time
}

func loadCatalogMediaWork(ctx context.Context, id, expected string) (*catalogMediaWork, error) {
	snapshot, err := (&CatalogSnapshotStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || snapshot.State != "received" || snapshot.ManifestSHA256 != expected {
		return nil, models.ErrCatalogSnapshotConflict
	}
	evidence, err := (&CatalogEvidenceImportStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if evidence == nil || evidence.State == "running" || evidence.ManifestSHA256 != expected {
		return nil, models.ErrCatalogSnapshotConflict
	}
	var body []byte
	if err := dbWrapper.Get(ctx, &body, "SELECT manifest FROM catalog_snapshots WHERE uuid=?", id); err != nil {
		return nil, err
	}
	manifest, err := scrape.PrepareCatalogSnapshot(body, expected)
	if err != nil {
		return nil, err
	}
	stamp, err := time.Parse(time.RFC3339Nano, snapshot.CapturedAt)
	if err != nil {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	return &catalogMediaWork{catalogRelationsWork: catalogRelationsWork{catalogEvidenceWork{snapshot: snapshot, manifest: manifest}}, stamp: stamp}, nil
}

func (s *CatalogMediaImportStore) Begin(ctx context.Context, input models.CatalogMediaBinding, now time.Time) (*models.CatalogMediaImport, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(input.SnapshotUUID) || !validSourceRunUUID(input.RootUUID) || !archive.ValidSHA256(input.ManifestSHA256) || !validJobTime(now) ||
		input.RootRevision < 1 || input.CollectionRevision < 1 || !validAccountText(input.LibraryRootPath, 4096, false) ||
		!filepath.IsAbs(input.LibraryRootPath) || filepath.Clean(input.LibraryRootPath) != input.LibraryRootPath {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	prior, err := s.Find(ctx, input.SnapshotUUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if !reflect.DeepEqual(prior.CatalogMediaBinding, input) {
			return nil, models.ErrCatalogSnapshotConflict
		}
		return prior, nil
	}
	w, err := loadCatalogMediaWork(ctx, input.SnapshotUUID, input.ManifestSHA256)
	if err != nil {
		return nil, err
	}
	root, err := (&MediaRootStore{}).Find(ctx, input.RootUUID)
	if err != nil {
		return nil, err
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, w.snapshot.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if root == nil || root.Revision != input.RootRevision || collection == nil || collection.Revision != input.CollectionRevision {
		return nil, models.ErrCatalogSnapshotConflict
	}
	// An already bound collection must agree with the reviewed logical root.
	if collection.RootUUID != nil && *collection.RootUUID != input.RootUUID {
		return nil, models.ErrCatalogSnapshotConflict
	}
	total := w.manifest.Tables["assets"].Rows + w.manifest.Tables["files"].Rows + w.manifest.Tables["appearances"].Rows
	stamp := now.UTC().Format(time.RFC3339Nano)
	_, err = dbWrapper.Exec(ctx, `INSERT INTO catalog_media_imports(snapshot_uuid,manifest_sha256,root_uuid,root_revision,collection_revision,library_root_path,policy,state,phase,source_records,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,'running','assets',?,?,?)`, input.SnapshotUUID, input.ManifestSHA256, input.RootUUID, input.RootRevision, input.CollectionRevision, input.LibraryRootPath, catalogMediaPolicy, total, stamp, stamp)
	if err != nil {
		return nil, err
	}
	return s.Find(ctx, input.SnapshotUUID)
}

func (w *catalogMediaWork) nextPhase() {
	switch w.progress.Phase {
	case "assets":
		w.progress.Phase = "files"
	case "files":
		w.progress.Phase = "appearances"
	case "appearances":
		w.progress.Phase, w.progress.State = "complete", "mapped"
		if w.progress.ReviewRecords > 0 {
			w.progress.State = "review"
		}
	}
	w.progress.LastOrdinal = 0
}

func (w *catalogMediaWork) importRow(ctx context.Context, row catalogEvidenceRow) (*models.CatalogMediaRecord, []byte, error) {
	record, err := w.decode(row)
	if err != nil {
		return nil, nil, err
	}
	result := &models.CatalogMediaRecord{Ordinal: row.Ordinal, Outcome: "mapped"}
	view := map[string]any{"policy": catalogMediaPolicy}
	switch record.Table {
	case "assets":
		err = w.asset(ctx, row, record, result)
	case "files":
		err = w.file(ctx, row, record, result, view)
	case "appearances":
		err = w.appearance(ctx, row, record, result, view)
	default:
		return nil, nil, models.ErrCatalogSnapshotInvalid
	}
	if err != nil {
		return nil, nil, err
	}
	body, err := archive.EncodeSourceJSON(view)
	if err != nil || len(body) > 65536 {
		return nil, nil, models.ErrCatalogSnapshotInvalid
	}
	return result, body, nil
}

func (s *CatalogMediaImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.CatalogMediaImport, error) {
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
	if prior == nil || prior.ManifestSHA256 != expected || prior.ProcessedRecords != after || prior.Policy != catalogMediaPolicy {
		return nil, models.ErrCatalogSnapshotConflict
	}
	if prior.State != "running" {
		return prior, nil
	}
	w, err := loadCatalogMediaWork(ctx, id, expected)
	if err != nil {
		return nil, err
	}
	w.progress = prior
	complete := snapshotAtomicWrite(ctx)
	for count := 0; count < 50 && w.bytes < 16<<20 && prior.State == "running"; {
		var row catalogEvidenceRow
		err := dbWrapper.Get(ctx, &row, `SELECT ordinal,data,data_sha256 FROM catalog_snapshot_records WHERE snapshot_uuid=? AND source_table=? AND ordinal>? ORDER BY ordinal LIMIT 1`, id, prior.Phase, prior.LastOrdinal)
		if errors.Is(err, sql.ErrNoRows) {
			w.nextPhase()
			continue
		}
		if err != nil {
			return nil, err
		}
		result, view, err := w.importRow(ctx, row)
		if err != nil {
			return nil, err
		}
		_, err = dbWrapper.Exec(ctx, `INSERT INTO catalog_media_records(snapshot_uuid,ordinal,claim_uuid,observation_uuid,match_uuid,post_uuid,post_file_uuid,media_evidence_uuid,outcome,reason,context_json)
 VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, row.Ordinal, result.ClaimUUID, result.ObservationUUID, result.MatchUUID, result.PostUUID, result.PostFileUUID, result.MediaEvidenceUUID, result.Outcome, result.Reason, string(view))
		if err != nil {
			return nil, err
		}
		prior.LastOrdinal, prior.ProcessedRecords = row.Ordinal, prior.ProcessedRecords+1
		if result.MatchUUID != nil && prior.Phase == "files" {
			prior.MatchedFiles++
		}
		if result.MediaEvidenceUUID != nil {
			prior.MediaAssociations++
		}
		switch result.Outcome {
		case "mapped":
			prior.MappedRecords++
		case "review":
			prior.ReviewRecords++
		case "unavailable":
			prior.UnavailableRecords++
		}
		count++
	}
	if prior.State != "running" && prior.ProcessedRecords != prior.TotalRecords {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	_, err = dbWrapper.Exec(ctx, `UPDATE catalog_media_imports SET state=?,phase=?,last_ordinal=?,processed_records=?,mapped_records=?,review_records=?,unavailable_records=?,matched_files=?,media_associations=?,updated_at=? WHERE snapshot_uuid=?`,
		prior.State, prior.Phase, prior.LastOrdinal, prior.ProcessedRecords, prior.MappedRecords, prior.ReviewRecords, prior.UnavailableRecords, prior.MatchedFiles, prior.MediaAssociations, now.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	*complete = true
	return ret, nil
}

func (s *CatalogMediaImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.CatalogMediaRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	columns := strings.Replace(catalogMediaColumns, "e.source_key", `CASE WHEN length(CAST(e.source_key AS BLOB))<=8192 THEN e.source_key ELSE '' END AS source_key,length(CAST(e.source_key AS BLOB))>8192 AS key_omitted`, 1)
	ret := []models.CatalogMediaRecord{}
	err := dbWrapper.Select(ctx, &ret, "SELECT "+columns+catalogMediaJoins+" WHERE r.snapshot_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?", id, after, limit)
	return ret, err
}

func (s *CatalogMediaImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.CatalogMediaRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	var row struct {
		models.CatalogMediaRecord
		Context string `db:"context_json"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+catalogMediaColumns+",r.context_json"+catalogMediaJoins+" WHERE r.snapshot_uuid=? AND r.ordinal=?", id, ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &models.CatalogMediaRecordDetails{CatalogMediaRecord: row.CatalogMediaRecord, Context: json.RawMessage(row.Context)}, nil
}

func (w *catalogMediaWork) id(row catalogEvidenceRow, kind string) string {
	return scrape.RegistryImportUUID(w.snapshot.UUID, "catalog-media:"+kind, strconv.FormatInt(row.Ordinal, 10))
}

func (w *catalogMediaWork) details(row catalogEvidenceRow) (json.RawMessage, error) {
	return archive.EncodeSourceJSON(map[string]any{"snapshot_uuid": w.snapshot.UUID, "source_ordinal": row.Ordinal, "source_sha256": row.SHA256})
}

func catalogMediaOutcome(result *models.CatalogMediaRecord, outcome, reason string) error {
	result.Outcome, result.Reason = outcome, reason
	return nil
}

func catalogMediaText(value any) (*string, bool) {
	if value == nil {
		return nil, true
	}
	text, ok := value.(string)
	return &text, ok
}

func catalogMediaInt(value any) (*int64, bool) {
	if value == nil {
		return nil, true
	}
	number, ok := value.(json.Number)
	if !ok {
		return nil, false
	}
	n, err := number.Int64()
	return &n, err == nil
}

func (w *catalogMediaWork) mappedRow(ctx context.Context, table, key string) (*models.CatalogMediaRecord, error) {
	body, err := archive.EncodeSourceJSON([]string{key})
	if err != nil {
		return nil, err
	}
	var ret models.CatalogMediaRecord
	err = dbWrapper.Get(ctx, &ret, "SELECT "+catalogMediaColumns+catalogMediaJoins+" WHERE e.snapshot_uuid=? AND e.source_table=? AND e.source_key=?", w.snapshot.UUID, table, string(body))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ret, err
}
