package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeAutomationCheckpointSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeCaptureRecordingTimeSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE automation_checkpoint_records; DROP TABLE automation_checkpoint_imports;
 DROP TABLE automation_checkpoint_bodies; DROP INDEX automation_enrichment_staged_input;
 DELETE FROM native_migration_history WHERE version=1000060`)
	require.NoError(t, err)
}

const legacyCheckpointFixture = `{"records":[{"kind":"post","metadata":{"category":"reddit","id":"receipt000","title":"Retained caption","author":{"id":9007199254740993},"source_extractor_url":"https://www.reddit.com/comments/receipt000"}},{"kind":"media","metadata":{"category":"reddit","id":"receipt000","title":"Retained caption","author":{"id":9007199254740993},"source_extractor_url":"https://www.reddit.com/comments/receipt000"}}],"pending_children":[{"url":"https://redgifs.com/watch/child","parent":{"category":"reddit","id":"receipt000","title":"Retained caption"},"depth":1,"reason":"rate_limited"}],"unresolved":[{"url":"https://outside.invalid/unknown","reason":"external_reference_only"}],"extractor_version":"1.32.15-dev","retry_reason":"rate_limited"}`

func checkpointImportFixture(t *testing.T, count int) *automationSnapshotFixture {
	t.Helper()
	catalog := enrichmentImportFixture(t, 1, nil)
	advanceCatalogEnrichment(t, catalog, 0)
	jobs := []map[string]any{legacyEnrichmentJob("reddit:post:aaa", "pending")}
	for i := 0; i < count; i++ {
		row := legacyEnrichmentJob(fmt.Sprintf("reddit:post:receipt%03d", i), "retry")
		row["staged_json"] = strings.Repeat(" ", i%2) + legacyCheckpointFixture
		jobs = append(jobs, row)
	}
	invalid := legacyEnrichmentJob("reddit:post:z-invalid", "retry")
	invalid["staged_json"] = `{broken JSON}`
	unknown := legacyEnrichmentJob("reddit:post:z-unknown", "retry")
	unknown["staged_json"] = `{"records":[],"future_format":true}`
	jobs = append(jobs, invalid, unknown)
	f := enrichmentAutomationFixture(t, catalog, map[string][]map[string]any{"enrichment_jobs": jobs})
	for p := advanceAutomationEnrichment(t, f, 0); p.State == "running"; {
		p = advanceAutomationEnrichment(t, f, p.LastOrdinal)
	}
	return f
}

func advanceCheckpointImport(t *testing.T, f *automationSnapshotFixture, after int64) *models.AutomationCheckpointImport {
	t.Helper()
	var result *models.AutomationCheckpointImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.AutomationCheckpointImport.Advance(ctx, f.manifest.UUID, f.sha, after, automationImportNow.Add(2*time.Hour))
		return err
	}))
	return result
}

func TestAutomationCheckpointImportResumesWithSharedBodiesAndExactSourceEvidence(t *testing.T) {
	f := checkpointImportFixture(t, 205)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"automation_snapshot_records", "automation_enrichment_imports", "automation_enrichment_records", "enrichment_targets", "enrichment_target_history", "source_captures", "source_post_revisions", "source_post_urls", "archive_jobs", "enrichment_job_attempts", "enrichment_checkpoints", "scenes", "images"} {
		before[table] = albumJobRows(t, raw, table)
	}
	p := advanceCheckpointImport(t, f, 0)
	require.Equal(t, "running", p.State)
	require.EqualValues(t, 207, p.TotalRecords)
	require.EqualValues(t, 100, p.ProcessedRecords)
	require.EqualValues(t, 101, p.LastOrdinal)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	p = advanceCheckpointImport(t, f, p.LastOrdinal)
	require.EqualValues(t, 200, p.ProcessedRecords)
	p = advanceCheckpointImport(t, f, p.LastOrdinal)
	require.Equal(t, "review", p.State)
	require.EqualValues(t, 205, p.MappedRecords)
	require.EqualValues(t, 2, p.ReviewRecords)
	require.False(t, p.Imported)
	require.Equal(t, p, advanceCheckpointImport(t, f, p.LastOrdinal))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM automation_checkpoint_bodies"))
	require.EqualValues(t, 4, queryUint(t, raw, "SELECT count(DISTINCT staged_sha256) FROM automation_checkpoint_records"))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.AutomationCheckpointImport.Records(ctx, f.manifest.UUID, 0, 2)
		require.NoError(t, err)
		require.Len(t, rows, 2)
		require.NotEqual(t, *rows[0].StagedSHA256, *rows[1].StagedSHA256)
		require.Equal(t, rows[0].BodySHA256, rows[1].BodySHA256)
		detail, err := f.repo.AutomationCheckpointImport.Record(ctx, f.manifest.UUID, rows[0].Ordinal)
		require.NoError(t, err)
		require.Contains(t, string(detail.SourceValues), "9007199254740993")
		require.Contains(t, string(detail.Body), "9007199254740993")
		require.NotContains(t, string(detail.Body), "observed_at")
		var staging scrape.EnrichmentStaging
		require.NoError(t, json.Unmarshal(detail.Body, &staging))
		require.Equal(t, "unrecorded", staging.ObservationTimeBasis)
		require.Equal(t, 0, staging.Records[1].Body)
		require.Equal(t, "https://redgifs.com/watch/child", staging.Pending[0].URL)
		bad, err := f.repo.AutomationCheckpointImport.Record(ctx, f.manifest.UUID, p.LastOrdinal-1)
		require.NoError(t, err)
		require.Equal(t, "invalid_legacy_checkpoint_json", bad.Reason)
		require.Nil(t, bad.Body)
		require.Contains(t, string(bad.SourceValues), "{broken JSON}")
		return nil
	}))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	backup := filepath.Join(t.TempDir(), "native.backup.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(backup))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		value, err := f.repo.AutomationCheckpointImport.Find(ctx, f.manifest.UUID)
		require.Equal(t, p, value)
		return err
	}))
	anonPath := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, anonPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	anon := openRawDB(t, anonPath)
	defer anon.Close()
	for _, table := range []string{"automation_checkpoint_records", "automation_checkpoint_imports", "automation_checkpoint_bodies"} {
		require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestAutomationCheckpointImportRejectsStaleBindingsAndRollsBackCaughtFailures(t *testing.T) {
	f := checkpointImportFixture(t, 2)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	for _, test := range []struct {
		digest string
		after  int64
		now    time.Time
	}{
		{strings.Repeat("f", 64), 0, automationImportNow.Add(2 * time.Hour)},
		{f.sha, 1, automationImportNow.Add(2 * time.Hour)},
		{f.sha, 0, automationImportNow},
	} {
		err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.AutomationCheckpointImport.Advance(ctx, f.manifest.UUID, test.digest, test.after, test.now)
			return err
		})
		require.ErrorIs(t, err, models.ErrAutomationSnapshotConflict)
	}
	_, err := raw.Exec(`CREATE TRIGGER fail_late_checkpoint BEFORE INSERT ON automation_checkpoint_records WHEN NEW.ordinal=3 BEGIN SELECT RAISE(ABORT,'fixture failure'); END`)
	require.NoError(t, err)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationCheckpointImport.Advance(ctx, f.manifest.UUID, f.sha, 0, automationImportNow.Add(2*time.Hour))
		require.ErrorContains(t, err, "fixture failure")
		return nil
	})
	require.Error(t, err)
	for _, table := range []string{"automation_checkpoint_imports", "automation_checkpoint_records", "automation_checkpoint_bodies"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	_, err = raw.Exec("DROP TRIGGER fail_late_checkpoint")
	require.NoError(t, err)
	p := advanceCheckpointImport(t, f, 0)
	require.EqualValues(t, 4, p.ProcessedRecords)
}

func TestAutomationCheckpointStartupRejectsAlteredProjectionWithoutWriting(t *testing.T) {
	for _, change := range []string{
		`DROP TRIGGER automation_checkpoint_body_immutable; UPDATE automation_checkpoint_bodies SET body=json_set(body,'$.observation_time_basis','source')`,
		`DROP TRIGGER automation_checkpoint_record_immutable; UPDATE automation_checkpoint_records SET staged_sha256=printf('%064d',0) WHERE ordinal=2`,
		`DELETE FROM automation_checkpoint_records WHERE ordinal=2`,
		`DROP INDEX automation_enrichment_staged_input`,
	} {
		t.Run(change, func(t *testing.T) {
			f := checkpointImportFixture(t, 1)
			advanceCheckpointImport(t, f, 0)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			_, err := raw.Exec(change)
			require.NoError(t, err)
			raw.Close()
			before, err := os.ReadFile(f.db.DatabasePath())
			require.NoError(t, err)
			require.Error(t, f.db.Open(f.db.DatabasePath()))
			after, err := os.ReadFile(f.db.DatabasePath())
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
