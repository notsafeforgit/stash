package sqlite_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func scanJournalFixture(t *testing.T, root string) models.ScanJournalInput {
	t.Helper()
	body, err := os.ReadFile("../scrape/testdata/legacy_scan_journal.json")
	require.NoError(t, err)
	return models.ScanJournalInput{UUID: uuid.NewString(), RootUUID: root, SourceUUID: uuid.NewString(), Document: body}
}

func TestScanJournalRetainsEveryFamilyAndNeverActivatesHistoricalRuntime(t *testing.T) {
	f := newSourceRunFixture(t)
	input := scanJournalFixture(t, f.root.UUID)
	var first *models.ScanJournal
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		first, err = f.repo.ScanJournal.Import(ctx, input, f.now)
		return err
	}))
	require.Equal(t, 8, first.RecordCount)
	var rows []models.ScanJournalRecord
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for after := int64(0); ; {
			page, err := f.repo.ScanJournal.Records(ctx, input.UUID, "", after, 2)
			if err != nil {
				return err
			}
			if len(page) == 0 {
				break
			}
			for _, record := range page {
				require.Empty(t, record.Evidence)
				detail, err := f.repo.ScanJournal.Record(ctx, record.UUID)
				if err != nil {
					return err
				}
				require.NotEmpty(t, detail.Evidence)
				rows = append(rows, *detail)
			}
			after = page[len(page)-1].Sequence
		}
		return nil
	}))
	require.Len(t, rows, 8)
	for _, row := range rows {
		switch row.Table {
		case "scan_jobs":
			require.Contains(t, string(row.Summary), `"legacy_date_min":"2026-09-20T12:34:56"`)
			require.Contains(t, string(row.Summary), `"attempts":3`)
			require.Contains(t, string(row.Summary), `"retry_after":1790899200.25`)
			require.Contains(t, string(row.Summary), `"deferral_uuid"`)
			require.NotContains(t, string(row.Summary), "/worker/bin")
		case "extractor_jobs":
			require.Contains(t, string(row.Summary), `"runtime_ownership":"not_transferred"`)
			if strings.Contains(string(row.Evidence), "manual:False") {
				require.Equal(t, "review", row.Disposition)
			} else {
				require.Contains(t, string(row.Summary), `"scan_record_uuid"`)
				require.Contains(t, string(row.Summary), `"last_index":257`)
			}
		case "legacy_handoffs":
			require.Equal(t, "review", row.Disposition)
		case "collection_backfill_completion":
			require.Contains(t, string(row.Evidence), "90071992547409931234")
			require.Equal(t, "historical", row.Disposition)
		}
	}
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(path))
	f.repo = f.db.Repository()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		replayed, err := f.repo.ScanJournal.Import(ctx, input, f.now)
		require.Equal(t, first, replayed)
		return err
	}))
	raw := openRawDB(t, path)
	defer raw.Close()
	for _, table := range []string{"source_runs", "source_run_attempts", "source_run_requests", "source_backfill_decisions"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	_, err := raw.Exec("UPDATE scan_journal_records SET disposition='historical'")
	require.Error(t, err)
	_, err = raw.Exec("DELETE FROM scan_journal_records WHERE id=(SELECT min(id) FROM scan_journal_records)")
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	broken := sqlite.NewDatabase()
	require.ErrorContains(t, broken.AuditForTesting(path), "incomplete retained scan journal evidence")
}

func TestScanJournalUnknownInputsConflictsAndLateFailuresRollBack(t *testing.T) {
	f := newSourceRunFixture(t)
	input := scanJournalFixture(t, f.root.UUID)
	for _, replace := range [][2]string{
		{`"external_tables"`, `"unknown_field"`},
		{`"scan_jobs":`, `"unknown_table":`},
		{`\"-i\"`, `\"--password\"`},
		{`"attempts":3`, `"attempts":-1`},
		{`"last_status":4`, `"last_status":true`},
	} {
		bad := input
		bad.Document = []byte(strings.ReplaceAll(string(input.Document), replace[0], replace[1]))
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.ScanJournal.Import(ctx, bad, f.now)
			require.ErrorIs(t, err, models.ErrScanJournalInvalid)
			return nil
		}))
	}
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if _, err := f.repo.ScanJournal.Import(ctx, input, f.now); err != nil {
			return err
		}
		changed := input
		changed.SourceUUID = uuid.NewString()
		_, err := f.repo.ScanJournal.Import(ctx, changed, f.now)
		return err
	}), models.ErrScanJournalConflict)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		found, err := f.repo.ScanJournal.Find(ctx, input.UUID)
		require.Nil(t, found)
		return err
	}))
	var document map[string]any
	require.NoError(t, json.Unmarshal(input.Document, &document))
	rows := document["tables"].(map[string]any)["extractor_jobs"].([]any)
	for _, key := range []string{"last_key", "last_index", "scope"} {
		delete(rows[0].(map[string]any), key)
	}
	var err error
	input.Document, err = json.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := f.repo.ScanJournal.Import(ctx, input, f.now); return err }))
}
