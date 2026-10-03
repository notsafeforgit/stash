package sqlite_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeAutomationEnrichmentSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	conn, err := raw.Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.ExecContext(t.Context(), "PRAGMA foreign_keys=OFF")
	require.NoError(t, err)
	defer func() {
		_, err := conn.ExecContext(t.Context(), "PRAGMA foreign_keys=ON")
		require.NoError(t, err)
	}()
	var dependents string
	for _, name := range []string{"enrichment_publication_scope", "enrichment_published_record_scope", "enrichment_job_success"} {
		var definition string
		require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_schema WHERE name=?", name).Scan(&definition))
		_, err := conn.ExecContext(t.Context(), "DROP TRIGGER "+name)
		require.NoError(t, err)
		dependents += definition + ";\n"
	}
	body, err := os.ReadFile("migrations/1000050_enrichment_work.up.sql")
	require.NoError(t, err)
	s := string(body)
	start := strings.Index(s, "CREATE TABLE enrichment_completions (")
	end := strings.Index(s, "CREATE TABLE enrichment_completion_captures (")
	create := s[start:strings.Index(s, "CREATE TRIGGER enrichment_completion_immutable")]
	create = strings.Replace(create, "CREATE TABLE enrichment_completions (", "CREATE TABLE enrichment_completions_prior (", 1)
	triggers := s[strings.Index(s, "CREATE TRIGGER enrichment_completion_immutable"):end]
	triggers += s[strings.Index(s, "CREATE TRIGGER enrichment_completion_capture_scope"):strings.Index(s, "CREATE TABLE enrichment_target_history")]
	_, err = conn.ExecContext(t.Context(), `DROP TRIGGER automation_enrichment_import_guard; DROP TRIGGER automation_enrichment_record_immutable;
 DROP TABLE automation_enrichment_records; DROP TABLE automation_enrichment_imports; DROP INDEX automation_enrichment_input;
 DROP TRIGGER enrichment_completion_immutable; DROP TRIGGER enrichment_completion_scope;
 DROP TRIGGER enrichment_completion_capture_scope; DROP TRIGGER enrichment_target_scope;`+create+`
 INSERT INTO enrichment_completions_prior(uuid,target_uuid,expected_revision,request_digest,capture_count,created_at)
 SELECT uuid,target_uuid,expected_revision,request_digest,capture_count,created_at FROM enrichment_completions;
 DROP TABLE enrichment_completions; ALTER TABLE enrichment_completions_prior RENAME TO enrichment_completions;`+triggers+dependents+`
 DELETE FROM native_migration_history WHERE version=1000058;`)
	require.NoError(t, err)
}

func legacyEnrichmentJob(post, status string) map[string]any {
	return map[string]any{"catalog_id": automationCatalog, "post_key": post, "version": 1, "platform": "reddit", "account_key": "reddit:handle:juniper",
		"url": "https://www.reddit.com/comments/" + strings.TrimPrefix(post, "reddit:post:"), "status": status, "priority": 40, "attempts": 7,
		"next_attempt": json.Number("0"), "last_error": "Original unavailable-source message", "staged_json": nil,
		"created_at": "2026-09-29T00:00:00Z", "updated_at": "2026-10-01T00:00:00Z"}
}

func advanceAutomationEnrichment(t *testing.T, f *automationSnapshotFixture, after int64) *models.AutomationEnrichmentImport {
	t.Helper()
	var ret *models.AutomationEnrichmentImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = f.repo.AutomationEnrichmentImport.Advance(ctx, f.manifest.UUID, f.sha, after, automationImportNow.Add(time.Hour))
		return err
	}))
	return ret
}

func TestAutomationEnrichmentImportHoldsPendingWorkAndRetainsProof(t *testing.T) {
	catalog := enrichmentImportFixture(t, 3, nil)
	advanceCatalogEnrichment(t, catalog, 0)
	jobs := []map[string]any{legacyEnrichmentJob("reddit:post:receipt000", "pending"), legacyEnrichmentJob("reddit:post:receipt001", "done"), legacyEnrichmentJob("reddit:post:receipt002", "excluded_source"), legacyEnrichmentJob("reddit:post:album", "done")}
	deadline := automationImportNow.Add(24 * time.Hour)
	tables := map[string][]map[string]any{
		"enrichment_jobs":            jobs,
		"enrichment_cooldowns":       {{"scope": "account:reddit:handle:juniper", "until_time": json.Number(fmt.Sprint(deadline.Unix())), "reason": "authentication"}},
		"enrichment_seed_progress":   {{"catalog_id": automationCatalog, "last_post_key": "reddit:post:receipt002", "complete": 1, "counts_json": `{"pending":1,"done":2}`}},
		"enrichment_source_progress": {{"platform": "reddit", "last_attempt": json.Number("1790856000.00000001")}},
	}
	f := enrichmentAutomationFixture(t, catalog, tables)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"source_captures", "source_post_revisions", "scenes", "images", "performers", "galleries", "archive_jobs", "enrichment_job_attempts"} {
		before[table] = albumJobRows(t, raw, table)
	}
	result := advanceAutomationEnrichment(t, f, 0)
	require.Equal(t, "review", result.State)
	require.EqualValues(t, 6, result.MappedRecords)
	require.EqualValues(t, 1, result.ReviewRecords)
	require.Equal(t, result, advanceAutomationEnrichment(t, f, result.LastOrdinal))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_targets WHERE state='held'"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_targets WHERE state='completed'"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_targets WHERE state='excluded'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_completion_captures"))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.AutomationEnrichmentImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		for _, r := range rows {
			if r.Disposition == "held" {
				target, err := f.repo.EnrichmentWork.Target(ctx, *r.TargetUUID)
				require.NoError(t, err)
				require.True(t, target.NotBefore.Equal(deadline))
			}
			if r.Disposition == "historical_completion" {
				completion, err := f.repo.EnrichmentWork.Completion(ctx, *r.CompletionUUID)
				require.NoError(t, err)
				require.Equal(t, "legacy_receipt", completion.Basis)
				require.Equal(t, r.ReceiptUUID, completion.LegacyReceiptUUID)
				require.Empty(t, completion.CaptureUUIDs)
			}
			if r.Outcome == "review" {
				require.Equal(t, "historical_completion_requires_evidence", r.Reason)
				require.Nil(t, r.TargetUUID)
			}
		}
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	backup := filepath.Join(t.TempDir(), "restored.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(backup))
	f.repo = f.db.Repository()
	require.Equal(t, result, advanceAutomationEnrichment(t, f, result.LastOrdinal))
	anonymous, err := sqlite.NewAnonymiser(f.db, filepath.Join(t.TempDir(), "anonymous.sqlite"))
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
}

func TestAutomationEnrichmentImportResumesAndPreservesNativeChoice(t *testing.T) {
	catalog := enrichmentImportFixture(t, 3, nil)
	advanceCatalogEnrichment(t, catalog, 0)
	jobs := []map[string]any{legacyEnrichmentJob("reddit:post:receipt000", "retry"), legacyEnrichmentJob("reddit:post:receipt001", "pending"), legacyEnrichmentJob("reddit:post:receipt002", "done")}
	jobs[1]["account_key"] = "reddit:handle:other"
	var cooldowns []map[string]any
	for i := range 200 {
		cooldowns = append(cooldowns, map[string]any{"scope": fmt.Sprintf("account:unrelated:%03d", i), "until_time": 1791118800, "reason": "authentication"})
	}
	accountDeadline, platformDeadline := automationImportNow.Add(24*time.Hour), automationImportNow.Add(12*time.Hour)
	cooldowns = append(cooldowns, map[string]any{"scope": "account:reddit:handle:juniper", "until_time": accountDeadline.Unix(), "reason": "authentication"}, map[string]any{"scope": "platform:reddit", "until_time": platformDeadline.Unix(), "reason": "rate_limited"})
	f := enrichmentAutomationFixture(t, catalog, map[string][]map[string]any{"enrichment_jobs": jobs, "enrichment_cooldowns": cooldowns})
	first := advanceAutomationEnrichment(t, f, 0)
	require.Equal(t, "running", first.State)
	require.EqualValues(t, 200, first.ProcessedRecords)
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationEnrichmentImport.Advance(ctx, f.manifest.UUID, f.sha, 0, automationImportNow.Add(time.Hour))
		return err
	}), models.ErrAutomationSnapshotConflict)
	ref, err := scrape.CatalogLocalPostReference(f.manifest.SourceUUID, automationCatalog, "reddit:post:receipt002")
	require.NoError(t, err)
	post := sourceTestPost(t, f.repo, ref, "")
	url, err := observePostURL(f.repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: jobs[2]["url"].(string)})
	require.NoError(t, err)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var collection string
	require.NoError(t, raw.QueryRow("SELECT collection_uuid FROM catalog_snapshots WHERE uuid=?", catalog.manifest.UUID).Scan(&collection))
	prior := retainEnrichment(t, f.repo, models.EnrichmentTargetInput{PostUUID: post.UUID, URLUUID: url.URLUUID, CollectionUUID: collection, CollectionRevision: 1, Policy: models.EnrichmentGalleryMetadataV1, Origin: "review"},
		models.EnrichmentSchedule{State: "excluded", Priority: 99, NotBefore: automationImportNow.Add(72 * time.Hour), Reason: "owner_choice"}, automationImportNow)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	result := advanceAutomationEnrichment(t, f, first.LastOrdinal)
	require.Equal(t, "mapped", result.State)
	require.EqualValues(t, 205, result.MappedRecords)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := f.repo.EnrichmentWork.Target(ctx, prior.UUID)
		require.NoError(t, err)
		require.Equal(t, prior, current)
		rows, err := f.repo.AutomationEnrichmentImport.Records(ctx, f.manifest.UUID, 202, 100)
		require.NoError(t, err)
		require.Len(t, rows, 3)
		require.True(t, rows[0].NotBefore.Equal(accountDeadline))
		require.True(t, rows[1].NotBefore.Equal(platformDeadline), "an account cooldown cannot pause a different account")
		require.Equal(t, "preserved", rows[2].Disposition)
		require.Nil(t, rows[2].CompletionUUID)
		return nil
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_completions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestAutomationEnrichmentImportReviewsStagingAndMissingProof(t *testing.T) {
	catalog := enrichmentImportFixture(t, 5, nil)
	advanceCatalogEnrichment(t, catalog, 0)
	jobs := []map[string]any{legacyEnrichmentJob("reddit:post:album", "already_native"), legacyEnrichmentJob("reddit:post:receipt000", "already_native"), legacyEnrichmentJob("reddit:post:receipt001", "done"), legacyEnrichmentJob("reddit:post:receipt002", "unsupported"), legacyEnrichmentJob("reddit:post:receipt003", "pending"), legacyEnrichmentJob("reddit:post:receipt004", "pending")}
	jobs[2]["staged_json"] = ` {"records":[{"original":9007199254740993}]} `
	jobs[4]["version"] = 9
	f := enrichmentAutomationFixture(t, catalog, map[string][]map[string]any{"enrichment_jobs": jobs})
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	ref, err := scrape.CatalogLocalPostReference(f.manifest.SourceUUID, automationCatalog, "reddit:post:receipt004")
	require.NoError(t, err)
	changed, err := raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=(SELECT post_uuid FROM source_post_identifiers WHERE namespace=? AND value=?)", ref.Namespace, ref.Value)
	require.NoError(t, err)
	count, err := changed.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	result := advanceAutomationEnrichment(t, f, 0)
	require.EqualValues(t, 1, result.MappedRecords)
	require.EqualValues(t, 5, result.ReviewRecords)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.AutomationEnrichmentImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		require.Equal(t, "source_present", rows[0].Disposition)
		completion, err := f.repo.EnrichmentWork.Completion(ctx, *rows[0].CompletionUUID)
		require.NoError(t, err)
		require.Equal(t, "legacy_capture", completion.Basis)
		require.Equal(t, rows[0].CaptureUUID, completion.LegacyCaptureUUID)
		require.Equal(t, "existing_source_requires_evidence", rows[1].Reason)
		require.Equal(t, "staged_review", rows[2].Disposition)
		require.Nil(t, rows[2].CompletionUUID)
		detail, err := f.repo.AutomationEnrichmentImport.Record(ctx, f.manifest.UUID, rows[2].Ordinal)
		require.NoError(t, err)
		require.Contains(t, string(detail.SourceValues), "9007199254740993")
		require.Equal(t, "legacy_unsupported", rows[3].Reason)
		require.Equal(t, "post_forgotten", rows[5].Reason)
		return nil
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_checkpoints"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestAutomationEnrichmentImportAtomicRollback(t *testing.T) {
	catalog := enrichmentImportFixture(t, 1, nil)
	advanceCatalogEnrichment(t, catalog, 0)
	f := enrichmentAutomationFixture(t, catalog, map[string][]map[string]any{"enrichment_jobs": {legacyEnrichmentJob("reddit:post:receipt000", "done")}})
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := albumJobRows(t, raw, "source_posts")
	attachmentSQL(t, f.db, "CREATE TRIGGER reject_import_record BEFORE INSERT ON automation_enrichment_records BEGIN SELECT RAISE(ABORT,'simulated failure'); END")
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationEnrichmentImport.Advance(ctx, f.manifest.UUID, f.sha, 0, automationImportNow.Add(time.Hour))
		require.ErrorContains(t, err, "simulated failure")
		return nil
	})
	require.ErrorContains(t, err, "did not finish atomically")
	for _, table := range []string{"enrichment_targets", "enrichment_completions", "enrichment_target_history", "automation_enrichment_imports", "automation_enrichment_records"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Equal(t, before, albumJobRows(t, raw, "source_posts"))
}

func TestAutomationEnrichmentImportCoalescesOnlyProvenAliases(t *testing.T) {
	for _, state := range []string{"pending", "coalesced"} {
		t.Run(state, func(t *testing.T) {
			catalog := relationFixture(t, nil)
			advanceCatalogEnrichment(t, catalog, 0)
			progress := advanceRelations(t, catalog, 0)
			for progress.State == "running" {
				progress = advanceRelations(t, catalog, progress.LastOrdinal)
			}
			jobs := []map[string]any{legacyEnrichmentJob(relationTwitterPost, "pending"), legacyEnrichmentJob("twitter:post:original-local-alias", state), legacyEnrichmentJob("twitter:post:124", "coalesced")}
			for _, job := range jobs {
				job["platform"], job["account_key"], job["url"] = "twitter", relationTwitterAccount, "https://x.com/source/status/123"
			}
			jobs[1]["priority"], jobs[1]["next_attempt"] = 90, automationImportNow.Add(24*time.Hour).Unix()
			f := enrichmentAutomationFixture(t, catalog, map[string][]map[string]any{"enrichment_jobs": jobs})
			result := advanceAutomationEnrichment(t, f, 0)
			require.EqualValues(t, 2, result.MappedRecords)
			require.EqualValues(t, 1, result.ReviewRecords)
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_targets"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_completions"))
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				rows, err := f.repo.AutomationEnrichmentImport.Records(ctx, f.manifest.UUID, 0, 100)
				require.NoError(t, err)
				require.Equal(t, "coalesced_post_requires_alias", rows[1].Reason)
				if state == "coalesced" {
					require.NotNil(t, rows[2].AliasOrdinal)
					require.Nil(t, rows[2].TargetUUID)
				} else {
					require.Equal(t, rows[0].TargetUUID, rows[2].TargetUUID)
					target, err := f.repo.EnrichmentWork.Target(ctx, *rows[2].TargetUUID)
					require.NoError(t, err)
					require.Equal(t, 90, target.Priority)
					require.True(t, target.NotBefore.Equal(automationImportNow.Add(24*time.Hour)))
				}
				return nil
			}))
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
		})
	}
}

func TestAutomationEnrichmentStartupRejectsChangedProjections(t *testing.T) {
	for _, test := range []struct{ name, trigger, change string }{
		{"attempts", "automation_enrichment_record_immutable", "UPDATE automation_enrichment_records SET historical_attempts=0 WHERE historical_attempts IS NOT NULL"},
		{"seed counts", "automation_enrichment_record_immutable", "UPDATE automation_enrichment_records SET seed_counts='{}' WHERE disposition='seed_progress'"},
		{"deadline", "automation_enrichment_record_immutable", "UPDATE automation_enrichment_records SET not_before='2020-01-01 00:00:00+00:00' WHERE disposition='held'"},
		{"cooldown", "automation_enrichment_record_immutable", "UPDATE automation_enrichment_records SET cooldown_value='another-account' WHERE disposition='cooldown'"},
		{"source binding", "automation_enrichment_record_immutable", "UPDATE automation_enrichment_records SET post_reference='post:'||lower(hex(zeroblob(32))) WHERE post_uuid IS NOT NULL"},
		{"completion digest", "enrichment_completion_immutable", "UPDATE enrichment_completions SET request_digest=lower(hex(zeroblob(32)))"},
		{"recorded time", "automation_enrichment_import_guard", "UPDATE automation_enrichment_imports SET created_at='2010-01-01T00:00:00Z'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := enrichmentImportFixture(t, 2, nil)
			advanceCatalogEnrichment(t, catalog, 0)
			f := enrichmentAutomationFixture(t, catalog, map[string][]map[string]any{
				"enrichment_jobs":          {legacyEnrichmentJob("reddit:post:receipt000", "pending"), legacyEnrichmentJob("reddit:post:receipt001", "done")},
				"enrichment_cooldowns":     {{"scope": "account:reddit:handle:juniper", "until_time": automationImportNow.Add(24 * time.Hour).Unix(), "reason": "authentication"}},
				"enrichment_seed_progress": {{"catalog_id": automationCatalog, "last_post_key": "reddit:post:receipt001", "complete": 1, "counts_json": `{"pending":1,"done":1}`}},
			})
			advanceAutomationEnrichment(t, f, 0)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			var definition string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", test.trigger).Scan(&definition))
			_, err := raw.Exec("DROP TRIGGER " + test.trigger + "; " + test.change + "; " + definition)
			require.NoError(t, err)
			require.Error(t, f.db.Open(f.db.DatabasePath()))
		})
	}
}

func TestAutomationEnrichmentMigrationPreservesNativePublication(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
			require.NoError(t, err)
			_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.NoError(t, err)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			defer raw.Close()
			before := map[string][][]any{}
			for _, table := range []string{"enrichment_targets", "enrichment_completions", "enrichment_target_history", "enrichment_publications", "enrichment_published_records", "source_captures", "archive_jobs", "archive_job_attempts"} {
				before[table] = albumJobRows(t, raw, table)
			}
			removeAutomationEnrichmentSchema(t, raw)
			_, err = raw.Exec("UPDATE schema_migrations SET version=1000057")
			require.NoError(t, err)
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(path), &needed))
			if collision {
				_, err = raw.Exec("CREATE TABLE automation_enrichment_imports(original TEXT); INSERT INTO automation_enrichment_imports VALUES('retained')")
				require.NoError(t, err)
				require.Error(t, f.db.RunAllMigrations())
				require.False(t, rawColumnExists(t, raw, "enrichment_completions", "legacy_receipt_uuid"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000058"))
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
				for table, rows := range before {
					require.Equal(t, rows, albumJobRows(t, raw, table), table)
				}
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
func enrichmentAutomationFixture(t *testing.T, catalog *catalogSnapshotFixture, tables map[string][]map[string]any) *automationSnapshotFixture {
	t.Helper()
	body, err := os.ReadFile("../scrape/testdata/automation_snapshot/manifest.json")
	require.NoError(t, err)
	var m scrape.AutomationSnapshotManifest
	require.NoError(t, json.Unmarshal(body, &m))
	m.SourceUUID, m.CapturedAt, m.Records = catalog.manifest.SourceUUID, "2026-10-02T14:00:00Z", 0
	m.Chunks, m.Integrity.ForeignKeyViolations = []scrape.CatalogSnapshotChunk{}, 0
	for name, descriptor := range m.Tables {
		descriptor.Rows, descriptor.Bytes, descriptor.SHA256 = 0, 0, scrape.CatalogSnapshotSHA(nil)
		m.Tables[name] = descriptor
	}
	f := &automationSnapshotFixture{db: catalog.db, repo: catalog.repo}
	var lines [][]byte
	for _, name := range []string{"enrichment_cooldowns", "enrichment_jobs", "enrichment_seed_progress", "enrichment_source_progress"} {
		table := struct {
			name string
			rows []map[string]any
		}{name, tables[name]}
		key := func(row map[string]any) []any {
			ret := []any{}
			for _, name := range m.Tables[table.name].Key {
				ret = append(ret, row[name])
			}
			return ret
		}
		sort.Slice(table.rows, func(i, j int) bool {
			return scrape.CatalogRecordAfter(table.name, key(table.rows[j]), table.name, key(table.rows[i]))
		})
		var data []byte
		for _, values := range table.rows {
			body, err := json.Marshal(map[string]any{"table": table.name, "key": key(values), "values": values})
			require.NoError(t, err)
			body = append(body, '\n')
			lines = append(lines, body)
			data = append(data, body...)
		}
		descriptor := m.Tables[table.name]
		descriptor.Rows, descriptor.Bytes, descriptor.SHA256 = int64(len(table.rows)), int64(len(data)), scrape.CatalogSnapshotSHA(data)
		m.Tables[table.name] = descriptor
		m.Records += descriptor.Rows
	}
	for len(lines) > 0 {
		count := min(100, len(lines))
		chunk := bytes.Join(lines[:count], nil)
		m.Chunks = append(m.Chunks, scrape.CatalogSnapshotChunk{File: fmt.Sprintf("records-%06d.jsonl", len(f.chunks)), Rows: count, Bytes: len(chunk), SHA256: scrape.CatalogSnapshotSHA(chunk)})
		f.chunks = append(f.chunks, chunk)
		lines = lines[count:]
	}
	f.setManifest(t, &m)
	f.begin(t)
	for index := range f.chunks {
		f.receive(t, index)
	}
	return f
}
