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
)

type AutomationSnapshotStore struct{}

const automationSnapshotSelect = `SELECT uuid,source_uuid,registry_import_uuid,manifest_sha256,source_sha256,captured_at,state,chunk_count,record_count,byte_count,next_chunk,received_records,received_bytes,created_at,updated_at FROM automation_snapshots WHERE uuid=?`

func (s *AutomationSnapshotStore) Find(ctx context.Context, id string) (*models.AutomationSnapshot, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	result := &models.AutomationSnapshot{PendingFamilies: []string{}}
	if err := dbWrapper.Get(ctx, result, automationSnapshotSelect, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := dbWrapper.Select(ctx, &result.PendingFamilies, "SELECT source_table FROM automation_snapshot_tables WHERE snapshot_uuid=? ORDER BY source_table", id); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *AutomationSnapshotStore) Begin(ctx context.Context, body []byte, expected string, now time.Time) (*models.AutomationSnapshot, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	m, err := scrape.PrepareAutomationSnapshot(body, expected)
	if err != nil {
		return nil, err
	}
	captured, _ := time.Parse(time.RFC3339Nano, m.CapturedAt)
	if !validJobTime(now) || captured.After(now.Add(time.Minute)) {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	if prior, err := s.Find(ctx, m.UUID); err != nil {
		return nil, err
	} else if prior != nil {
		if prior.ManifestSHA256 != expected {
			return nil, models.ErrAutomationSnapshotConflict
		}
		return prior, nil
	}
	var used bool
	if err := dbWrapper.Get(ctx, &used, "SELECT EXISTS(SELECT 1 FROM automation_snapshots WHERE source_uuid=?)", m.SourceUUID); err != nil {
		return nil, err
	}
	if used {
		return nil, models.ErrAutomationSnapshotConflict
	}
	var registry string
	if err := dbWrapper.Get(ctx, &registry, "SELECT uuid FROM catalog_registry_imports WHERE source_uuid=?", m.SourceUUID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrAutomationSnapshotConflict
		}
		return nil, err
	}
	var size int64
	for _, table := range m.Tables {
		size += table.Bytes
	}
	state := "receiving"
	if len(m.Chunks) == 0 {
		state = "received"
	}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO automation_snapshots(uuid,source_uuid,registry_import_uuid,manifest_sha256,source_sha256,manifest,captured_at,state,chunk_count,record_count,byte_count,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, m.UUID, m.SourceUUID, registry, expected, m.SourceSHA256, body, m.CapturedAt, state, len(m.Chunks), m.Records, size, stamp, stamp); err != nil {
		return nil, err
	}
	hashState, err := sha256.New().(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		return nil, err
	}
	for name := range m.Tables {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO automation_snapshot_tables(snapshot_uuid,source_table,received_sha256,hash_state) VALUES(?,?,?,?)`, m.UUID, name, scrape.CatalogSnapshotSHA(nil), hashState); err != nil {
			return nil, err
		}
	}
	result, err := s.Find(ctx, m.UUID)
	*complete = err == nil
	return result, err
}

func (s *AutomationSnapshotStore) Receive(ctx context.Context, id, expected string, index int, body []byte, now time.Time) (*models.AutomationSnapshot, error) {
	if err := receiveSnapshotChunk(ctx, automationSnapshotCodec(), id, expected, index, body, now); err != nil {
		return nil, err
	}
	return s.Find(ctx, id)
}
