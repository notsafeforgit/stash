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

type CatalogEnrichmentImportStore struct{}

const catalogEnrichmentPolicy = "catalog-enrichment-v1"
const catalogEnrichmentColumns = `r.ordinal,e.source_key,e.data_sha256,r.post_uuid,r.receipt_uuid,r.outcome,r.reason`
const catalogEnrichmentJoins = ` FROM catalog_enrichment_records r JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal`

func (s *CatalogEnrichmentImportStore) Find(ctx context.Context, id string) (*models.CatalogEnrichmentImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.CatalogEnrichmentImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM catalog_enrichment_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

func (s *CatalogEnrichmentImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.CatalogEnrichmentImport, error) {
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
	if (prior == nil && after != 0) || (prior != nil && (prior.LastOrdinal != after || prior.ManifestSHA256 != expected || prior.Policy != catalogEnrichmentPolicy)) {
		return nil, models.ErrCatalogSnapshotConflict
	}
	if prior != nil && prior.State != "running" {
		return prior, nil
	}
	// Received snapshot and completed post evidence are required. Historical
	// receipts neither schedule work nor invent native execution proof.
	w, err := loadCompletedCatalogRelations(ctx, id, expected)
	if err != nil {
		return nil, err
	}
	captured, err := time.Parse(time.RFC3339Nano, w.snapshot.CapturedAt)
	if err != nil || now.Before(captured) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	if prior != nil {
		updated, err := time.Parse(time.RFC3339Nano, prior.UpdatedAt)
		if err != nil || now.Before(updated) {
			return nil, models.ErrCatalogSnapshotConflict
		}
	}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		var historical bool
		if err := dbWrapper.Get(ctx, &historical, "SELECT EXISTS(SELECT 1 FROM source_collection_revisions WHERE collection_uuid=? AND revision=1 AND origin='migration')", w.snapshot.CollectionUUID); err != nil {
			return nil, err
		}
		if !historical {
			return nil, models.ErrCatalogSnapshotConflict
		}
		_, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_enrichment_imports(snapshot_uuid,manifest_sha256,collection_uuid,collection_revision,policy,state,source_records,created_at,updated_at)
 VALUES(?,?,?,1,?,'running',?,?,?)`, id, expected, w.snapshot.CollectionUUID, catalogEnrichmentPolicy, w.manifest.Tables["enrichment_receipts"].Rows, stamp, stamp)
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
		err := dbWrapper.Get(ctx, &row, "SELECT ordinal,data,data_sha256 FROM catalog_snapshot_records WHERE snapshot_uuid=? AND source_table='enrichment_receipts' AND ordinal>? ORDER BY ordinal LIMIT 1", id, prior.LastOrdinal)
		if errors.Is(err, sql.ErrNoRows) {
			if prior.ProcessedRecords != prior.TotalRecords {
				return nil, models.ErrCatalogSnapshotInvalid
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
		record, err := w.decode(row)
		if err != nil {
			return nil, err
		}
		result, err := catalogEnrichmentRecord(ctx, w, prior, row, record, stamp)
		if err != nil {
			return nil, err
		}
		_, err = dbWrapper.Exec(ctx, `INSERT INTO catalog_enrichment_records(snapshot_uuid,ordinal,post_uuid,receipt_uuid,outcome,reason) VALUES(?,?,?,?,?,?)`, id, row.Ordinal, result.PostUUID, result.ReceiptUUID, result.Outcome, result.Reason)
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
	_, err = dbWrapper.Exec(ctx, `UPDATE catalog_enrichment_imports SET state=?,last_ordinal=?,processed_records=?,mapped_records=?,review_records=?,updated_at=? WHERE snapshot_uuid=?`, prior.State, prior.LastOrdinal, prior.ProcessedRecords, prior.MappedRecords, prior.ReviewRecords, stamp, id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, id)
	*complete = err == nil
	return ret, err
}

func (s *CatalogEnrichmentImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.CatalogEnrichmentRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	columns := strings.Replace(catalogEnrichmentColumns, "e.source_key", `CASE WHEN length(CAST(e.source_key AS BLOB))<=8192 THEN e.source_key ELSE '' END AS source_key,length(CAST(e.source_key AS BLOB))>8192 AS key_omitted`, 1)
	ret := []models.CatalogEnrichmentRecord{}
	err := dbWrapper.Select(ctx, &ret, "SELECT "+columns+catalogEnrichmentJoins+" WHERE r.snapshot_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?", id, after, limit)
	return ret, err
}

func (s *CatalogEnrichmentImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.CatalogEnrichmentRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	var row struct {
		models.CatalogEnrichmentRecord
		Data string `db:"data"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+catalogEnrichmentColumns+",e.data"+catalogEnrichmentJoins+" WHERE r.snapshot_uuid=? AND r.ordinal=?", id, ordinal)
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
	return &models.CatalogEnrichmentRecordDetails{CatalogEnrichmentRecord: row.CatalogEnrichmentRecord, SourceValues: values}, err
}
