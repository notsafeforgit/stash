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
)

// These prefixes and decoders are private, fixed application definitions. No
// caller-supplied SQL identifiers or source DDL enter the receiving queries.
type snapshotCodec struct {
	prefix            string
	invalid, conflict error
	prepare           func([]byte, string) (*snapshotManifest, error)
}

type snapshotManifest struct {
	tables  map[string]scrape.CatalogSnapshotTable
	chunks  []scrape.CatalogSnapshotChunk
	records int64
	record  func([]byte) (*scrape.CatalogSnapshotRecord, error)
}

func catalogSnapshotCodec() snapshotCodec {
	return snapshotCodec{"catalog", models.ErrCatalogSnapshotInvalid, models.ErrCatalogSnapshotConflict,
		func(body []byte, expected string) (*snapshotManifest, error) {
			m, err := scrape.PrepareCatalogSnapshot(body, expected)
			if err != nil {
				return nil, err
			}
			return &snapshotManifest{m.Tables, m.Chunks, m.Records, m.Record}, nil
		}}
}

func automationSnapshotCodec() snapshotCodec {
	return snapshotCodec{"automation", models.ErrAutomationSnapshotInvalid, models.ErrAutomationSnapshotConflict,
		func(body []byte, expected string) (*snapshotManifest, error) {
			m, err := scrape.PrepareAutomationSnapshot(body, expected)
			if err != nil {
				return nil, err
			}
			return &snapshotManifest{m.Tables, m.Chunks, m.Records, m.Record}, nil
		}}
}

type snapshotTableProgress struct {
	Rows   int64  `db:"received_records"`
	Bytes  int64  `db:"received_bytes"`
	Digest string `db:"received_sha256"`
	State  []byte `db:"hash_state"`
	Hash   hash.Hash
}

func receiveSnapshotChunk(ctx context.Context, codec snapshotCodec, id, expected string, index int, body []byte, now time.Time) error {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return err
	}
	if !validSourceRunUUID(id) || !archive.ValidSHA256(expected) || index < 0 || len(body) == 0 || len(body) > scrape.CatalogChunkLimit || !validJobTime(now) {
		return codec.invalid
	}
	var row struct {
		Manifest []byte `db:"manifest"`
		SHA256   string `db:"manifest_sha256"`
		Next     int    `db:"next_chunk"`
		Received int64  `db:"received_records"`
		Table    string `db:"last_table"`
		Key      string `db:"last_key"`
		Updated  string `db:"updated_at"`
	}
	header, tables, chunks, records := codec.prefix+"_snapshots", codec.prefix+"_snapshot_tables", codec.prefix+"_snapshot_chunks", codec.prefix+"_snapshot_records"
	if err := dbWrapper.Get(ctx, &row, "SELECT manifest,manifest_sha256,next_chunk,received_records,last_table,last_key,updated_at FROM "+header+" WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return codec.conflict
		}
		return err
	}
	if row.SHA256 != expected || index > row.Next {
		return codec.conflict
	}
	m, err := codec.prepare(row.Manifest, expected)
	if err != nil {
		return err
	}
	if index >= len(m.chunks) {
		return codec.invalid
	}
	c := m.chunks[index]
	if len(body) != c.Bytes || scrape.CatalogSnapshotSHA(body) != c.SHA256 {
		return codec.invalid
	}
	if index < row.Next {
		return nil
	}
	updated, err := time.Parse(time.RFC3339Nano, row.Updated)
	if err != nil || now.Before(updated) {
		return codec.conflict
	}
	var last []any
	decoder := json.NewDecoder(bytes.NewBufferString(row.Key))
	decoder.UseNumber()
	if err := decoder.Decode(&last); err != nil {
		return err
	}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if _, err := dbWrapper.Exec(ctx, "INSERT INTO "+chunks+"(snapshot_uuid,chunk_index,sha256,record_count,byte_count,created_at) VALUES(?,?,?,?,?,?)", id, index, c.SHA256, c.Rows, c.Bytes, stamp); err != nil {
		return err
	}
	progress := map[string]*snapshotTableProgress{}
	count := 0
	for len(body) != 0 {
		end := bytes.IndexByte(body, '\n')
		if end < 0 || count >= c.Rows {
			return codec.invalid
		}
		r, err := m.record(body[:end+1])
		if err != nil {
			return err
		}
		if !scrape.CatalogRecordAfter(r.Table, r.Key, row.Table, last) {
			return codec.invalid
		}
		p := progress[r.Table]
		if p == nil {
			p = &snapshotTableProgress{Hash: sha256.New()}
			if err := dbWrapper.Get(ctx, p, "SELECT received_records,received_bytes,received_sha256,hash_state FROM "+tables+" WHERE snapshot_uuid=? AND source_table=?", id, r.Table); err != nil {
				return err
			}
			if err := p.Hash.(encoding.BinaryUnmarshaler).UnmarshalBinary(p.State); err != nil {
				return err
			}
			if hexencoding.EncodeToString(p.Hash.Sum(nil)) != p.Digest {
				return codec.invalid
			}
			progress[r.Table] = p
		}
		p.Rows++
		p.Bytes += int64(len(r.Line))
		_, _ = p.Hash.Write(r.Line)
		descriptor := m.tables[r.Table]
		if p.Rows > descriptor.Rows || p.Bytes > descriptor.Bytes || (p.Rows == descriptor.Rows && (p.Bytes != descriptor.Bytes || hexencoding.EncodeToString(p.Hash.Sum(nil)) != descriptor.SHA256)) {
			return codec.invalid
		}
		count++
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO "+records+"(snapshot_uuid,ordinal,chunk_index,source_table,source_key,data,byte_count,data_sha256) VALUES(?,?,?,?,?,?,?,?)",
			id, row.Received+int64(count), index, r.Table, r.KeyJSON, string(r.Line), len(r.Line), scrape.CatalogSnapshotSHA(r.Line)); err != nil {
			return err
		}
		row.Table, row.Key, last = r.Table, r.KeyJSON, r.Key
		body = body[end+1:]
	}
	if count != c.Rows {
		return codec.invalid
	}
	for name, p := range progress {
		state, err := p.Hash.(encoding.BinaryMarshaler).MarshalBinary()
		if err != nil {
			return err
		}
		if _, err := dbWrapper.Exec(ctx, "UPDATE "+tables+" SET received_records=?,received_bytes=?,received_sha256=?,hash_state=? WHERE snapshot_uuid=? AND source_table=?", p.Rows, p.Bytes, hexencoding.EncodeToString(p.Hash.Sum(nil)), state, id, name); err != nil {
			return err
		}
	}
	state := "receiving"
	if index+1 == len(m.chunks) {
		var invalid bool
		if err := dbWrapper.Get(ctx, &invalid, `SELECT EXISTS(SELECT 1 FROM `+tables+` t, `+header+` s,
 json_each(CAST(s.manifest AS TEXT),'$.tables') expected WHERE t.snapshot_uuid=s.uuid AND s.uuid=? AND expected.key=t.source_table
 AND (t.received_records!=json_extract(expected.value,'$.rows') OR t.received_bytes!=json_extract(expected.value,'$.bytes') OR t.received_sha256!=json_extract(expected.value,'$.sha256')))`, id); err != nil {
			return err
		}
		if invalid || row.Received+int64(c.Rows) != m.records {
			return codec.invalid
		}
		state = "received"
	}
	if _, err := dbWrapper.Exec(ctx, "UPDATE "+header+" SET state=?,next_chunk=?,received_records=received_records+?,received_bytes=received_bytes+?,last_table=?,last_key=?,updated_at=? WHERE uuid=?",
		state, index+1, c.Rows, c.Bytes, row.Table, row.Key, stamp, id); err != nil {
		return err
	}
	*complete = true
	return nil
}
