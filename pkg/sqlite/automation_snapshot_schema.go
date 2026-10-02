package sqlite

import (
	"bytes"
	"crypto/sha256"
	"encoding"
	hexencoding "encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func validateAutomationSnapshotSchema(conn *sqlx.DB) error {
	objects := map[string]string{
		"automation_snapshots": "table", "automation_snapshot_tables": "table",
		"automation_snapshot_chunks": "table", "automation_snapshot_records": "table",
		"automation_snapshot_identity_immutable": "trigger", "automation_snapshot_chunk_immutable": "trigger",
		"automation_snapshot_record_immutable": "trigger", "automation_snapshot_record_chunk": "index",
		"automation_snapshot_record_table": "index",
	}
	for name, kind := range objects {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=? AND type=?)", name, kind); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	// The lineage connection is already read-only. One read transaction permits
	// nested bounded queries without borrowing its single connection again.
	tx, err := conn.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var invalid bool
	if err := tx.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM automation_snapshots s
 LEFT JOIN catalog_registry_imports i ON i.uuid=s.registry_import_uuid WHERE i.uuid IS NULL OR i.source_uuid!=s.source_uuid)
 OR EXISTS(SELECT 1 FROM automation_snapshot_tables t LEFT JOIN automation_snapshots s ON s.uuid=t.snapshot_uuid WHERE s.uuid IS NULL)
 OR EXISTS(SELECT 1 FROM automation_snapshot_chunks c LEFT JOIN automation_snapshots s ON s.uuid=c.snapshot_uuid WHERE s.uuid IS NULL)
 OR EXISTS(SELECT 1 FROM automation_snapshot_records r LEFT JOIN automation_snapshots s ON s.uuid=r.snapshot_uuid WHERE s.uuid IS NULL)`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has orphaned automation snapshot records or registry bindings")
	}
	rows, err := tx.Queryx("SELECT * FROM automation_snapshots ORDER BY uuid")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row automationSnapshotRow
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if err := validateAutomationSnapshot(tx, row); err != nil {
			return fmt.Errorf("native automation snapshot %s: %w", row.UUID, err)
		}
	}
	return rows.Err()
}

type automationSnapshotRow struct {
	models.AutomationSnapshot
	Manifest  []byte `db:"manifest"`
	LastTable string `db:"last_table"`
	LastKey   string `db:"last_key"`
}

func validateAutomationSnapshot(tx *sqlx.Tx, row automationSnapshotRow) error {
	bad := errors.New("incomplete or corrupt automation snapshot receipt")
	m, err := scrape.PrepareAutomationSnapshot(row.Manifest, row.ManifestSHA256)
	if err != nil {
		return err
	}
	created, e1 := time.Parse(time.RFC3339Nano, row.CreatedAt)
	updated, e2 := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	captured, _ := time.Parse(time.RFC3339Nano, m.CapturedAt)
	if e1 != nil || e2 != nil || !validJobTime(created) || updated.Before(created) || captured.After(created.Add(time.Minute)) ||
		row.UUID != m.UUID || row.SourceUUID != m.SourceUUID || row.SourceSHA256 != m.SourceSHA256 || row.CapturedAt != m.CapturedAt ||
		row.Records != m.Records || row.Chunks != len(m.Chunks) || row.NextChunk < 0 || row.NextChunk > len(m.Chunks) ||
		(row.State != "receiving" && row.State != "received") || (row.State == "received") != (row.NextChunk == len(m.Chunks)) {
		return bad
	}
	var size int64
	for _, table := range m.Tables {
		size += table.Bytes
	}
	if row.Bytes != size {
		return bad
	}
	var chunkCount, recordCount int64
	if err := tx.Get(&chunkCount, "SELECT count(*) FROM automation_snapshot_chunks WHERE snapshot_uuid=?", row.UUID); err != nil {
		return err
	}
	if err := tx.Get(&recordCount, "SELECT count(*) FROM automation_snapshot_records WHERE snapshot_uuid=?", row.UUID); err != nil {
		return err
	}
	if chunkCount != int64(row.NextChunk) || recordCount != row.ReceivedRecords {
		return bad
	}
	progress := map[string]*snapshotTableProgress{}
	for name := range m.Tables {
		progress[name] = &snapshotTableProgress{Hash: sha256.New()}
	}
	var lastTable string
	lastKey := "[]"
	var last []any
	var records, receivedBytes int64
	for index := 0; index < row.NextChunk; index++ {
		var chunk struct {
			Rows    int    `db:"record_count"`
			Bytes   int    `db:"byte_count"`
			SHA256  string `db:"sha256"`
			Created string `db:"created_at"`
		}
		if err := tx.Get(&chunk, "SELECT record_count,byte_count,sha256,created_at FROM automation_snapshot_chunks WHERE snapshot_uuid=? AND chunk_index=?", row.UUID, index); err != nil {
			return err
		}
		expected := m.Chunks[index]
		stamp, err := time.Parse(time.RFC3339Nano, chunk.Created)
		if err != nil || stamp.Before(created) || stamp.After(updated) || chunk.Rows != expected.Rows || chunk.Bytes != expected.Bytes || chunk.SHA256 != expected.SHA256 {
			return bad
		}
		rows, err := tx.Queryx("SELECT ordinal,source_table,source_key,data,byte_count,data_sha256 FROM automation_snapshot_records WHERE snapshot_uuid=? AND chunk_index=? ORDER BY ordinal", row.UUID, index)
		if err != nil {
			return err
		}
		hashed := sha256.New()
		var count, bytesRead int
		check := func() error {
			defer rows.Close()
			for rows.Next() {
				var original struct {
					Ordinal int64  `db:"ordinal"`
					Table   string `db:"source_table"`
					Key     string `db:"source_key"`
					Data    string `db:"data"`
					Bytes   int    `db:"byte_count"`
					SHA256  string `db:"data_sha256"`
				}
				if err := rows.StructScan(&original); err != nil {
					return err
				}
				records++
				count++
				line := []byte(original.Data)
				bytesRead += len(line)
				if original.Ordinal != records || original.Bytes != len(line) || count > chunk.Rows || bytesRead > chunk.Bytes || scrape.CatalogSnapshotSHA(line) != original.SHA256 {
					return bad
				}
				r, err := m.Record(line)
				if err != nil || r.Table != original.Table || r.KeyJSON != original.Key || !scrape.CatalogRecordAfter(r.Table, r.Key, lastTable, last) {
					return bad
				}
				lastTable, lastKey, last = r.Table, r.KeyJSON, r.Key
				_, _ = hashed.Write(line)
				p := progress[r.Table]
				p.Rows++
				p.Bytes += int64(len(line))
				_, _ = p.Hash.Write(line)
			}
			return rows.Err()
		}()
		if check != nil {
			return check
		}
		if count != chunk.Rows || bytesRead != chunk.Bytes || hexencoding.EncodeToString(hashed.Sum(nil)) != chunk.SHA256 {
			return bad
		}
		receivedBytes += int64(bytesRead)
	}
	if records != row.ReceivedRecords || receivedBytes != row.ReceivedBytes || lastTable != row.LastTable || lastKey != row.LastKey {
		return bad
	}
	var tableCount int
	if err := tx.Get(&tableCount, "SELECT count(*) FROM automation_snapshot_tables WHERE snapshot_uuid=?", row.UUID); err != nil {
		return err
	}
	if tableCount != len(m.Tables) {
		return bad
	}
	for name, p := range progress {
		var stored snapshotTableProgress
		if err := tx.Get(&stored, "SELECT received_records,received_bytes,received_sha256,hash_state FROM automation_snapshot_tables WHERE snapshot_uuid=? AND source_table=?", row.UUID, name); err != nil {
			return err
		}
		state, err := p.Hash.(encoding.BinaryMarshaler).MarshalBinary()
		if err != nil {
			return err
		}
		expected := m.Tables[name]
		actual := hexencoding.EncodeToString(p.Hash.Sum(nil))
		if stored.Rows != p.Rows || stored.Bytes != p.Bytes || stored.Digest != actual || !bytes.Equal(stored.State, state) ||
			p.Rows > expected.Rows || p.Bytes > expected.Bytes || ((row.State == "received" || p.Rows == expected.Rows) &&
			(p.Rows != expected.Rows || p.Bytes != expected.Bytes || actual != expected.SHA256)) {
			return bad
		}
	}
	return nil
}
