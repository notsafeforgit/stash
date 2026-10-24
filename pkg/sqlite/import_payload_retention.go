package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

const importPayloadRetentionPolicy = "catalog-payload-retention-v1"

func importPayloadProgress(ctx context.Context, id string) (*models.ImportPayloadRetirement, error) {
	ret := &models.ImportPayloadRetirement{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM catalog_payload_retirements WHERE snapshot_uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func (s *ArchiveImportStore) PayloadRetention(ctx context.Context, kind, id string) (*models.ImportPayloadRetention, error) {
	details, err := s.Snapshot(ctx, kind, id)
	if err != nil || details == nil {
		return nil, err
	}
	ret := &models.ImportPayloadRetention{Kind: kind, SnapshotUUID: id}
	if kind == "automation" {
		// These inputs remain necessary for activation/provenance validation,
		// even after individual translators finish. Do not treat mapped as a
		// blanket permission to discard other consumers' source evidence.
		ret.Reason = "automation_activation_and_provenance"
		return ret, nil
	}
	ret.Progress, err = importPayloadProgress(ctx, id)
	if err != nil {
		return nil, err
	}
	err = dbWrapper.Get(ctx, &ret.Eligible, "SELECT EXISTS(SELECT 1 FROM catalog_retirable_snapshots WHERE uuid=?)", id)
	if err != nil {
		return nil, err
	}
	if !ret.Eligible {
		ret.Reason = "incomplete_imports_or_historical_reviews"
		return ret, nil
	}
	var totals struct {
		Records int64
		Bytes   int64
	}
	err = dbWrapper.Get(ctx, &totals, `SELECT count(*) records,coalesce(sum(r.byte_count-2),0) bytes
FROM catalog_retirable_payloads p JOIN catalog_snapshot_records r ON r.snapshot_uuid=p.snapshot_uuid AND r.ordinal=p.ordinal
WHERE p.snapshot_uuid=?`, id)
	ret.CandidateRecords, ret.CandidateBytes = totals.Records, totals.Bytes
	return ret, err
}

func (s *ArchiveImportStore) RetirePayloads(ctx context.Context, id string, after int64, limit int, now time.Time) (*models.ImportPayloadRetirement, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 512 || !validJobTime(now) {
		return nil, models.ErrArchiveImportInvalid
	}
	prior, err := importPayloadProgress(ctx, id)
	if err != nil {
		return nil, err
	}
	if prior != nil && after <= prior.LastOrdinal && (after < prior.LastOrdinal || prior.State == "complete") {
		return prior, nil
	}
	if (prior == nil && after != 0) || (prior != nil && after != prior.LastOrdinal) {
		return nil, models.ErrImportPayloadRetentionConflict
	}
	var eligible bool
	if err := dbWrapper.Get(ctx, &eligible, "SELECT EXISTS(SELECT 1 FROM catalog_retirable_snapshots WHERE uuid=?)", id); err != nil {
		return nil, err
	}
	if !eligible {
		return nil, models.ErrImportPayloadRetentionConflict
	}
	var snapshot struct {
		Manifest        []byte
		ManifestSHA256  string `db:"manifest_sha256"`
		ReceivedRecords int64  `db:"received_records"`
	}
	if err := dbWrapper.Get(ctx, &snapshot, "SELECT manifest,manifest_sha256,received_records FROM catalog_snapshots WHERE uuid=?", id); err != nil {
		return nil, err
	}
	manifest, err := scrape.PrepareCatalogSnapshot(snapshot.Manifest, snapshot.ManifestSHA256)
	if err != nil {
		return nil, err
	}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		_, err = dbWrapper.Exec(ctx, `INSERT INTO catalog_payload_retirements(snapshot_uuid,policy,manifest_sha256,state,created_at,updated_at)
VALUES(?,?,?,'running',?,?)`, id, importPayloadRetentionPolicy, snapshot.ManifestSHA256, stamp, stamp)
		if err != nil {
			return nil, err
		}
	}
	type input struct {
		Ordinal int64
		Data    string
		SHA256  string `db:"data_sha256"`
		Table   string `db:"source_table"`
		Key     string `db:"source_key"`
		Bytes   int    `db:"byte_count"`
	}
	var page []input
	rows, err := dbWrapper.Queryx(ctx, `SELECT r.ordinal,r.data,r.data_sha256,r.source_table,r.source_key,r.byte_count
FROM catalog_retirable_payloads p JOIN catalog_snapshot_records r ON r.snapshot_uuid=p.snapshot_uuid AND r.ordinal=p.ordinal
WHERE p.snapshot_uuid=? AND p.ordinal>? ORDER BY p.ordinal LIMIT ?`, id, after, limit)
	if err != nil {
		return nil, err
	}
	err = func() error {
		defer rows.Close()
		bytes := 0
		for rows.Next() {
			var row input
			if err := rows.StructScan(&row); err != nil {
				return err
			}
			if bytes+row.Bytes > scrape.CatalogChunkLimit && len(page) > 0 {
				break
			}
			bytes += row.Bytes
			page = append(page, row)
		}
		return rows.Err()
	}()
	if err != nil {
		return nil, err
	}
	last := after
	for _, row := range page {
		body := []byte(row.Data)
		if len(body) != row.Bytes || scrape.CatalogSnapshotSHA(body) != row.SHA256 {
			return nil, models.ErrCatalogSnapshotInvalid
		}
		record, err := manifest.StoredRecord(body)
		if err != nil || record.Table != row.Table || record.KeyJSON != row.Key {
			return nil, models.ErrCatalogSnapshotInvalid
		}
		_, err = dbWrapper.Exec(ctx, `UPDATE catalog_snapshot_records SET original_byte_count=byte_count,
retained_asset_id=json_extract(data,'$.values.asset_id'),data='{}',byte_count=2
WHERE snapshot_uuid=? AND ordinal=?`, id, row.Ordinal)
		if err != nil {
			return nil, err
		}
		last = row.Ordinal
	}
	var remaining bool
	if err := dbWrapper.Get(ctx, &remaining, "SELECT EXISTS(SELECT 1 FROM catalog_retirable_payloads WHERE snapshot_uuid=? AND ordinal>?)", id, last); err != nil {
		return nil, err
	}
	state := "running"
	if !remaining {
		state, last = "complete", snapshot.ReceivedRecords
	}
	_, err = dbWrapper.Exec(ctx, "UPDATE catalog_payload_retirements SET last_ordinal=?,state=?,updated_at=? WHERE snapshot_uuid=?", last, state, stamp, id)
	if err != nil {
		return nil, err
	}
	ret, err := importPayloadProgress(ctx, id)
	*complete = err == nil
	return ret, err
}
