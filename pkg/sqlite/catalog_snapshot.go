package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding"
	hexencoding "encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"time"

	"github.com/stashapp/stash/pkg/archive"
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
			return errors.New("catalog snapshot write did not finish atomically")
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

type catalogTableProgress struct {
	Rows  int64  `db:"received_records"`
	Bytes int64  `db:"received_bytes"`
	State []byte `db:"hash_state"`
	Hash  hash.Hash
}

func (s *CatalogSnapshotStore) Receive(ctx context.Context, id, expected string, index int, body []byte, now time.Time) (*models.CatalogSnapshot, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || !archive.ValidSHA256(expected) || index < 0 || len(body) == 0 || len(body) > scrape.CatalogChunkLimit || !validJobTime(now) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	receipt, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if receipt == nil || receipt.ManifestSHA256 != expected || index > receipt.NextChunk {
		return nil, models.ErrCatalogSnapshotConflict
	}
	var row struct {
		Manifest []byte `db:"manifest"`
		Table    string `db:"last_table"`
		Key      string `db:"last_key"`
	}
	if err := dbWrapper.Get(ctx, &row, "SELECT manifest,last_table,last_key FROM catalog_snapshots WHERE uuid=?", id); err != nil {
		return nil, err
	}
	m, err := scrape.PrepareCatalogSnapshot(row.Manifest, expected)
	if err != nil {
		return nil, err
	}
	if index >= len(m.Chunks) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	c := m.Chunks[index]
	if len(body) != c.Bytes || scrape.CatalogSnapshotSHA(body) != c.SHA256 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	if index < receipt.NextChunk {
		return receipt, nil
	}
	var last []any
	decoder := json.NewDecoder(bytes.NewBufferString(row.Key))
	decoder.UseNumber()
	if err := decoder.Decode(&last); err != nil {
		return nil, err
	}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_snapshot_chunks(snapshot_uuid,chunk_index,sha256,record_count,byte_count,created_at) VALUES(?,?,?,?,?,?)`, id, index, c.SHA256, c.Rows, c.Bytes, stamp); err != nil {
		return nil, err
	}
	progress := map[string]*catalogTableProgress{}
	count := 0
	for len(body) != 0 {
		end := bytes.IndexByte(body, '\n')
		if end < 0 || count >= c.Rows {
			return nil, models.ErrCatalogSnapshotInvalid
		}
		r, err := m.Record(body[:end+1])
		if err != nil {
			return nil, err
		}
		if !scrape.CatalogRecordAfter(r.Table, r.Key, row.Table, last) {
			return nil, models.ErrCatalogSnapshotInvalid
		}
		p := progress[r.Table]
		if p == nil {
			p = &catalogTableProgress{Hash: sha256.New()}
			if err := dbWrapper.Get(ctx, p, `SELECT received_records,received_bytes,hash_state FROM catalog_snapshot_tables WHERE snapshot_uuid=? AND source_table=?`, id, r.Table); err != nil {
				return nil, err
			}
			if err := p.Hash.(encoding.BinaryUnmarshaler).UnmarshalBinary(p.State); err != nil {
				return nil, err
			}
			progress[r.Table] = p
		}
		p.Rows++
		p.Bytes += int64(len(r.Line))
		_, _ = p.Hash.Write(r.Line)
		descriptor := m.Tables[r.Table]
		if p.Rows > descriptor.Rows || p.Bytes > descriptor.Bytes || (p.Rows == descriptor.Rows && (p.Bytes != descriptor.Bytes || hexencoding.EncodeToString(p.Hash.Sum(nil)) != descriptor.SHA256)) {
			return nil, models.ErrCatalogSnapshotInvalid
		}
		count++
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_snapshot_records(snapshot_uuid,ordinal,chunk_index,source_table,source_key,data,byte_count,data_sha256) VALUES(?,?,?,?,?,?,?,?)`,
			id, receipt.ReceivedRecords+int64(count), index, r.Table, r.KeyJSON, string(r.Line), len(r.Line), scrape.CatalogSnapshotSHA(r.Line)); err != nil {
			return nil, err
		}
		row.Table, row.Key, last = r.Table, r.KeyJSON, r.Key
		body = body[end+1:]
	}
	if count != c.Rows {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	for name, p := range progress {
		state, err := p.Hash.(encoding.BinaryMarshaler).MarshalBinary()
		if err != nil {
			return nil, err
		}
		if _, err := dbWrapper.Exec(ctx, `UPDATE catalog_snapshot_tables SET received_records=?,received_bytes=?,received_sha256=?,hash_state=? WHERE snapshot_uuid=? AND source_table=?`, p.Rows, p.Bytes, hexencoding.EncodeToString(p.Hash.Sum(nil)), state, id, name); err != nil {
			return nil, err
		}
	}
	state := "receiving"
	if index+1 == len(m.Chunks) {
		var invalid bool
		if err := dbWrapper.Get(ctx, &invalid, `SELECT EXISTS(SELECT 1 FROM catalog_snapshot_tables t, catalog_snapshots s,
 json_each(CAST(s.manifest AS TEXT),'$.tables') expected WHERE t.snapshot_uuid=s.uuid AND s.uuid=? AND expected.key=t.source_table
 AND (t.received_records!=json_extract(expected.value,'$.rows') OR t.received_bytes!=json_extract(expected.value,'$.bytes') OR t.received_sha256!=json_extract(expected.value,'$.sha256')))`, id); err != nil {
			return nil, err
		}
		if invalid || receipt.ReceivedRecords+int64(c.Rows) != m.Records {
			return nil, models.ErrCatalogSnapshotInvalid
		}
		state = "received"
	}
	if _, err := dbWrapper.Exec(ctx, `UPDATE catalog_snapshots SET state=?,next_chunk=?,received_records=received_records+?,received_bytes=received_bytes+?,last_table=?,last_key=?,updated_at=? WHERE uuid=?`,
		state, index+1, c.Rows, c.Bytes, row.Table, row.Key, stamp, id); err != nil {
		return nil, err
	}
	result, err := s.Find(ctx, id)
	*complete = err == nil
	return result, err
}
