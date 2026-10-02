package sqlite_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

var automationImportNow = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

type automationSnapshotFixture struct {
	db       *sqlite.Database
	repo     models.Repository
	body     []byte
	manifest *scrape.AutomationSnapshotManifest
	sha      string
	chunks   [][]byte
}

func automationFixture(t *testing.T, empty bool) *automationSnapshotFixture {
	t.Helper()
	db, repo, registry, _ := registryFixture(t, nil)
	registryApply(t, repo, registry, registryPreview(t, repo, registry))
	body, err := os.ReadFile("../scrape/testdata/automation_snapshot/manifest.json")
	require.NoError(t, err)
	var m scrape.AutomationSnapshotManifest
	require.NoError(t, json.Unmarshal(body, &m))
	m.SourceUUID = registry.SourceUUID
	f := &automationSnapshotFixture{db: db, repo: repo}
	if empty {
		m.Records, m.Integrity.ForeignKeyViolations = 0, 0
		for name, table := range m.Tables {
			table.Rows, table.Bytes, table.SHA256 = 0, 0, scrape.CatalogSnapshotSHA(nil)
			m.Tables[name] = table
		}
	} else {
		body, err := os.ReadFile("../scrape/testdata/automation_snapshot/records-000000.jsonl")
		require.NoError(t, err)
		lines := bytes.SplitAfter(body, []byte{'\n'})
		for len(lines) > 1 {
			end := min(2, len(lines)-1)
			f.chunks = append(f.chunks, bytes.Join(lines[:end], nil))
			lines = lines[end:]
		}
	}
	m.Chunks = []scrape.CatalogSnapshotChunk{}
	for i, chunk := range f.chunks {
		m.Chunks = append(m.Chunks, scrape.CatalogSnapshotChunk{File: fmt.Sprintf("records-%06d.jsonl", i), Rows: bytes.Count(chunk, []byte{'\n'}), Bytes: len(chunk), SHA256: scrape.CatalogSnapshotSHA(chunk)})
	}
	f.setManifest(t, &m)
	return f
}

func (f *automationSnapshotFixture) setManifest(t *testing.T, m *scrape.AutomationSnapshotManifest) {
	t.Helper()
	var err error
	f.body, err = json.Marshal(m)
	require.NoError(t, err)
	f.sha = scrape.CatalogSnapshotSHA(f.body)
	f.manifest, err = scrape.PrepareAutomationSnapshot(f.body, f.sha)
	require.NoError(t, err)
}

func (f *automationSnapshotFixture) begin(t *testing.T) *models.AutomationSnapshot {
	t.Helper()
	var result *models.AutomationSnapshot
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.AutomationSnapshot.Begin(ctx, f.body, f.sha, automationImportNow)
		return err
	}))
	return result
}

func (f *automationSnapshotFixture) receive(t *testing.T, index int) *models.AutomationSnapshot {
	t.Helper()
	var result *models.AutomationSnapshot
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.AutomationSnapshot.Receive(ctx, f.manifest.UUID, f.sha, index, f.chunks[index], automationImportNow.Add(time.Minute))
		return err
	}))
	return result
}

func TestAutomationSnapshotRestartsReplaysAndNeverActivates(t *testing.T) {
	f := automationFixture(t, false)
	start := f.begin(t)
	require.Equal(t, "receiving", start.State)
	require.Len(t, start.PendingFamilies, 10)
	require.False(t, start.Imported)
	require.NotEmpty(t, start.RegistryImportUUID)
	first := f.receive(t, 0)
	require.Equal(t, first, f.receive(t, 0))
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationSnapshot.Receive(ctx, f.manifest.UUID, f.sha, 0, bytes.Replace(f.chunks[0], []byte("legacy"), []byte("change"), 1), automationImportNow)
		return err
	}), models.ErrAutomationSnapshotInvalid)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, first, f.begin(t))
	last := first
	for index := range f.chunks {
		last = f.receive(t, index)
	}
	require.Equal(t, "received", last.State)
	require.False(t, last.Imported)
	require.EqualValues(t, 14, last.ReceivedRecords)
	require.Equal(t, start.PendingFamilies, last.PendingFamilies)
	require.Equal(t, last, f.begin(t))
	require.Equal(t, last, f.receive(t, 0))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	raw := openRawDB(t, f.db.DatabasePath())
	var lines []string
	rows, err := raw.Query("SELECT data FROM automation_snapshot_records ORDER BY ordinal")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		lines = append(lines, line)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Equal(t, bytes.Join(f.chunks, nil), []byte(strings.Join(lines, "")))
	for _, table := range []string{"archive_jobs", "translation_requests", "translation_targets", "source_posts", "source_captures"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	_, err = raw.Exec("UPDATE automation_snapshot_records SET data='{}'")
	require.ErrorContains(t, err, "immutable")
	require.NoError(t, raw.Close())
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	raw = openRawDB(t, output)
	defer raw.Close()
	for _, table := range []string{"automation_snapshots", "automation_snapshot_tables", "automation_snapshot_chunks", "automation_snapshot_records"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
}

func TestAutomationSnapshotConcurrentReceiptsAndIndexedChunkValidation(t *testing.T) {
	f := automationFixture(t, false)
	f.begin(t)
	var wg sync.WaitGroup
	errors := make(chan error, 6)
	for range 6 {
		wg.Go(func() {
			errors <- f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.AutomationSnapshot.Receive(ctx, f.manifest.UUID, f.sha, 0, f.chunks[0], automationImportNow)
				return err
			})
		})
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.Equal(t, 1, f.begin(t).NextChunk)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM automation_snapshot_records"))
	rows, err := raw.Query("EXPLAIN QUERY PLAN SELECT ordinal,source_table,source_key,data,byte_count,data_sha256 FROM automation_snapshot_records WHERE snapshot_uuid=? AND chunk_index=? ORDER BY ordinal", f.manifest.UUID, 0)
	require.NoError(t, err)
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plan.WriteString(detail)
	}
	require.NoError(t, rows.Err())
	require.Contains(t, plan.String(), "SEARCH automation_snapshot_records USING INDEX automation_snapshot_record_chunk (snapshot_uuid=? AND chunk_index=?)")
	require.NotContains(t, plan.String(), "TEMP B-TREE")
}

func TestAutomationSnapshotEmptyAndConflictingSources(t *testing.T) {
	f := automationFixture(t, true)
	start := f.begin(t)
	require.Equal(t, "received", start.State)
	require.Zero(t, start.Records)
	require.Len(t, start.PendingFamilies, 10)
	require.False(t, start.Imported)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, start, f.begin(t))
	for _, alter := range []func(*scrape.AutomationSnapshotManifest){
		func(m *scrape.AutomationSnapshotManifest) { m.UUID = uuid.NewString() },
		func(m *scrape.AutomationSnapshotManifest) { m.SourceSHA256 = strings.Repeat("0", 64) },
		func(m *scrape.AutomationSnapshotManifest) { m.UUID, m.SourceUUID = uuid.NewString(), uuid.NewString() },
	} {
		var m scrape.AutomationSnapshotManifest
		require.NoError(t, json.Unmarshal(f.body, &m))
		alter(&m)
		body, err := json.Marshal(m)
		require.NoError(t, err)
		require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.AutomationSnapshot.Begin(ctx, body, scrape.CatalogSnapshotSHA(body), automationImportNow)
			return err
		}), models.ErrAutomationSnapshotConflict)
	}
}

func TestAutomationSnapshotFailuresAreAtomic(t *testing.T) {
	f := automationFixture(t, false)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER fail_automation_begin BEFORE INSERT ON automation_snapshot_tables
 WHEN NEW.source_table='translation_jobs' BEGIN SELECT RAISE(ABORT,'fixture begin failure'); END`)
	require.NoError(t, err)
	require.ErrorContains(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationSnapshot.Begin(ctx, f.body, f.sha, automationImportNow)
		require.ErrorContains(t, err, "fixture begin failure")
		return nil
	}), "snapshot write did not finish atomically")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM automation_snapshots"))
	_, err = raw.Exec("DROP TRIGGER fail_automation_begin")
	require.NoError(t, err)
	f.begin(t)
	_, err = raw.Exec(`CREATE TRIGGER fail_automation_chunk BEFORE INSERT ON automation_snapshot_records
 WHEN NEW.ordinal=2 BEGIN SELECT RAISE(ABORT,'fixture chunk failure'); END`)
	require.NoError(t, err)
	require.ErrorContains(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationSnapshot.Receive(ctx, f.manifest.UUID, f.sha, 0, f.chunks[0], automationImportNow)
		require.ErrorContains(t, err, "fixture chunk failure")
		return nil
	}), "snapshot write did not finish atomically")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM automation_snapshot_records"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM automation_snapshot_chunks"))
	require.Zero(t, f.begin(t).NextChunk)
	_, err = raw.Exec("DROP TRIGGER fail_automation_chunk")
	require.NoError(t, err)
	f.receive(t, 0)
}

func TestAutomationSnapshotRejectsGapsAlterationAndFalseTableDigests(t *testing.T) {
	f := automationFixture(t, false)
	m := f.manifest
	broken := m.Tables["discovery_accounts"]
	broken.SHA256 = strings.Repeat("0", 64)
	m.Tables["discovery_accounts"] = broken
	f.setManifest(t, m)
	f.begin(t)
	for _, test := range []struct {
		index int
		body  []byte
		err   error
	}{
		{1, f.chunks[1], models.ErrAutomationSnapshotConflict},
		{0, bytes.Replace(f.chunks[0], []byte("legacy"), []byte("change"), 1), models.ErrAutomationSnapshotInvalid},
		{0, f.chunks[0], models.ErrAutomationSnapshotInvalid},
	} {
		require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.AutomationSnapshot.Receive(ctx, f.manifest.UUID, f.sha, test.index, test.body, automationImportNow)
			return err
		}), test.err)
	}
	require.Zero(t, f.begin(t).NextChunk)
}

func TestAutomationSnapshotCorruptionIsRefusedBeforeWrites(t *testing.T) {
	for _, test := range []struct{ name, trigger, sql string }{
		{"hash state", "", "UPDATE automation_snapshot_tables SET hash_state=zeroblob(108)"},
		{"checkpoint", "", `UPDATE automation_snapshots SET last_key='["wrong"]'`},
		{"record bytes", "automation_snapshot_record_immutable", "UPDATE automation_snapshot_records SET data=replace(data,'legacy error','legacy wrong') WHERE source_table='translation_jobs'"},
		{"record key", "automation_snapshot_record_immutable", `UPDATE automation_snapshot_records SET source_key='["wrong"]' WHERE ordinal=1`},
		{"chunk time", "automation_snapshot_chunk_immutable", "UPDATE automation_snapshot_chunks SET created_at='bad' WHERE chunk_index=0"},
		{"manifest hash", "automation_snapshot_identity_immutable", "UPDATE automation_snapshots SET manifest_sha256='" + strings.Repeat("0", 64) + "'"},
		{"missing record", "", "DELETE FROM automation_snapshot_records WHERE ordinal=1"},
		{"missing index", "", "DROP INDEX automation_snapshot_record_chunk"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := automationFixture(t, false)
			f.begin(t)
			for index := range f.chunks {
				f.receive(t, index)
			}
			require.NoError(t, f.db.Close())
			path := f.db.DatabasePath()
			raw := openRawDB(t, path)
			var trigger string
			if test.trigger != "" {
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", test.trigger).Scan(&trigger))
				_, err := raw.Exec("DROP TRIGGER " + test.trigger)
				require.NoError(t, err)
			}
			_, err := raw.Exec(test.sql)
			require.NoError(t, err)
			if trigger != "" {
				_, err = raw.Exec(trigger)
				require.NoError(t, err)
			}
			require.NoError(t, raw.Close())
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Error(t, f.db.Open(path))
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
