package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/txn"
)

type CatalogSnapshotStore struct{}

const catalogSnapshotSelect = `SELECT uuid,source_uuid,registry_import_uuid,catalog_id,collection_uuid,manifest_sha256,captured_at,state,chunk_count,record_count,byte_count,next_chunk,received_records,received_bytes,created_at,updated_at FROM catalog_snapshots WHERE uuid=?`

func (s *CatalogSnapshotStore) Find(ctx context.Context, id string) (*models.CatalogSnapshot, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.CatalogSnapshot{PendingFamilies: []string{}}
	if err := dbWrapper.Get(ctx, ret, catalogSnapshotSelect, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	// Even fully received families still require semantic/domain reconciliation.
	if err := dbWrapper.Select(ctx, &ret.PendingFamilies, "SELECT source_table FROM catalog_snapshot_tables WHERE snapshot_uuid=? ORDER BY source_table", id); err != nil {
		return nil, err
	}
	return ret, nil
}

func snapshotAtomicWrite(ctx context.Context) *bool {
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return errors.New("snapshot write did not finish atomically")
		}
		return nil
	})
	return &complete
}

func (s *CatalogSnapshotStore) Begin(ctx context.Context, body []byte, expected string, now time.Time) (*models.CatalogSnapshot, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	m, err := scrape.PrepareCatalogSnapshot(body, expected)
	if err != nil {
		return nil, err
	}
	captured, _ := time.Parse(time.RFC3339Nano, m.CapturedAt)
	if !validJobTime(now) || captured.After(now.Add(time.Minute)) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	if prior, err := s.Find(ctx, m.UUID); err != nil {
		return nil, err
	} else if prior != nil {
		if prior.ManifestSHA256 != expected {
			return nil, models.ErrCatalogSnapshotConflict
		}
		return prior, nil
	}
	var used bool
	if err := dbWrapper.Get(ctx, &used, "SELECT EXISTS(SELECT 1 FROM catalog_snapshots WHERE source_uuid=? AND catalog_id=?)", m.SourceUUID, m.CatalogID); err != nil {
		return nil, err
	}
	if used {
		return nil, models.ErrCatalogSnapshotConflict
	}
	var mapping struct {
		Collection string `db:"collection_uuid"`
		Import     string `db:"import_uuid"`
	}
	if err := dbWrapper.Get(ctx, &mapping, `SELECT m.collection_uuid,m.import_uuid FROM catalog_collection_mappings m
 JOIN catalog_registry_imports i ON i.uuid=m.import_uuid AND i.source_uuid=m.source_uuid WHERE m.source_uuid=? AND m.catalog_id=?`, m.SourceUUID, m.CatalogID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrCatalogSnapshotConflict
		}
		return nil, err
	}
	var size int64
	for _, table := range m.Tables {
		size += table.Bytes
	}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_snapshots(uuid,source_uuid,registry_import_uuid,catalog_id,collection_uuid,manifest_sha256,manifest,captured_at,state,chunk_count,record_count,byte_count,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,?,'receiving',?,?,?,?,?)`, m.UUID, m.SourceUUID, mapping.Import, m.CatalogID, mapping.Collection, expected, body, m.CapturedAt, len(m.Chunks), m.Records, size, stamp, stamp); err != nil {
		return nil, err
	}
	state, err := sha256.New().(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		return nil, err
	}
	for name := range m.Tables {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_snapshot_tables(snapshot_uuid,source_table,received_sha256,hash_state) VALUES(?,?,?,?)`, m.UUID, name, scrape.CatalogSnapshotSHA(nil), state); err != nil {
			return nil, err
		}
	}
	result, err := s.Find(ctx, m.UUID)
	*complete = err == nil
	return result, err
}

func (s *CatalogSnapshotStore) Receive(ctx context.Context, id, expected string, index int, body []byte, now time.Time) (*models.CatalogSnapshot, error) {
	if err := receiveSnapshotChunk(ctx, catalogSnapshotCodec(), id, expected, index, body, now); err != nil {
		return nil, err
	}
	return s.Find(ctx, id)
}
