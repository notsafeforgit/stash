package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type ScanJournalStore struct{}

type scanJournalRow struct {
	models.ScanJournal
	InventoryText string `db:"inventory"`
}

func (r scanJournalRow) resolve() *models.ScanJournal {
	r.Inventory = json.RawMessage(r.InventoryText)
	return &r.ScanJournal
}

type scanJournalRecordRow struct {
	models.ScanJournalRecord
	SummaryText  string `db:"summary"`
	EvidenceText string `db:"evidence"`
}

func (r scanJournalRecordRow) resolve() models.ScanJournalRecord {
	r.Summary = json.RawMessage(r.SummaryText)
	if r.EvidenceText != "" {
		r.Evidence = json.RawMessage(r.EvidenceText)
	}
	return r.ScanJournalRecord
}

func (s *ScanJournalStore) Find(ctx context.Context, id string) (*models.ScanJournal, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrScanJournalInvalid
	}
	var row scanJournalRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM scan_journals WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func (s *ScanJournalStore) Import(ctx context.Context, input models.ScanJournalInput, now time.Time) (*models.ScanJournal, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) {
		return nil, models.ErrScanJournalInvalid
	}
	prepared, err := scrape.PrepareScanJournal(input)
	if err != nil {
		return nil, err
	}
	prior, err := s.Find(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.RootUUID != input.RootUUID || prior.SourceUUID != input.SourceUUID || prior.InputSHA256 != prepared.Receipt.InputSHA256 {
			return nil, models.ErrScanJournalConflict
		}
		return prior, nil
	}
	root, err := (&MediaRootStore{}).Find(ctx, input.RootUUID)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, models.ErrScanJournalInvalid
	}
	r := &prepared.Receipt
	r.CreatedAt = now.UTC().Format(time.RFC3339Nano)
	if _, err = dbWrapper.Exec(ctx, `INSERT INTO scan_journals(uuid,root_uuid,source_uuid,input_sha256,captured_at,record_count,inventory,created_at)
VALUES(?,?,?,?,?,?,?,?)`, r.UUID, r.RootUUID, r.SourceUUID, r.InputSHA256, r.CapturedAt, r.RecordCount, string(r.Inventory), r.CreatedAt); err != nil {
		return nil, err
	}
	for _, record := range prepared.Records {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO scan_journal_records(uuid,journal_uuid,source_table,source_key,context,target_url,disposition,summary,evidence)
VALUES(?,?,?,?,?,?,?,?,?)`, record.UUID, record.JournalUUID, record.Table, record.SourceKey, record.Context, record.TargetURL, record.Disposition, string(record.Summary), string(record.Evidence)); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (s *ScanJournalStore) Record(ctx context.Context, id string) (*models.ScanJournalRecord, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrScanJournalInvalid
	}
	var row scanJournalRecordRow
	if err := dbWrapper.Get(ctx, &row, "SELECT * FROM scan_journal_records WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	result := row.resolve()
	return &result, nil
}

func (s *ScanJournalStore) Records(ctx context.Context, journal, table string, after int64, limit int) ([]models.ScanJournalRecord, error) {
	if !validSourceRunUUID(journal) || (table != "" && !scrape.ValidScanJournalTable(table)) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrScanJournalInvalid
	}
	where, args := "", []any{journal, after}
	if table != "" {
		where = " AND source_table=?"
		args = append(args, table)
	}
	args = append(args, limit)
	var rows []scanJournalRecordRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT id,uuid,journal_uuid,source_table,source_key,context,target_url,disposition,summary
FROM scan_journal_records WHERE journal_uuid=? AND id>?`+where+" ORDER BY id LIMIT ?", args...); err != nil {
		return nil, err
	}
	result := make([]models.ScanJournalRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.resolve())
	}
	return result, nil
}
