package sqlite_test

import (
	"bytes"
	"context"
	"encoding/json"
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

const automationCatalog = "c_11111111111111111111111111111111"

func automationJob(t *testing.T, original, status string, result any) map[string]any {
	t.Helper()
	key, err := scrape.LegacyCatalogJSON([]any{original, "en"}, 65536)
	require.NoError(t, err)
	var cached any
	if result != nil {
		body, err := json.Marshal(result)
		require.NoError(t, err)
		cached = string(body)
	}
	return map[string]any{"job_key": scrape.CatalogSnapshotSHA(key), "original_text": original, "target_language": "en", "priority": 25,
		"source_hint": nil, "status": status, "result_json": cached, "attempts": 3, "next_attempt": 1791032400.000001,
		"last_error": "Historical retry", "created_at": "2026-09-29T00:00:00Z", "updated_at": "2026-09-30T00:00:00Z"}
}

func automationTarget(job map[string]any, post, field string, applied int) map[string]any {
	return map[string]any{"job_key": job["job_key"], "catalog_id": automationCatalog, "post_key": post, "field": field, "applied": applied}
}

func translationAutomationFixture(t *testing.T, jobs, targets []map[string]any) *automationSnapshotFixture {
	t.Helper()
	catalog := snapshotFixture(t)
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
	for _, table := range []struct {
		name string
		rows []map[string]any
	}{{"translation_jobs", jobs}, {"translation_targets", targets}} {
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
	for _, target := range targets {
		post := target["post_key"].(string)
		if post == "missing" || target["catalog_id"] != automationCatalog {
			continue
		}
		ref, err := scrape.CatalogLocalPostReference(m.SourceUUID, automationCatalog, post)
		require.NoError(t, err)
		sourceTestPost(t, f.repo, ref, "")
	}
	return f
}

func advanceAutomationTranslations(t *testing.T, f *automationSnapshotFixture, after int64) *models.AutomationTranslationImport {
	t.Helper()
	var result *models.AutomationTranslationImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.AutomationTranslationImport.Advance(ctx, f.manifest.UUID, f.sha, after, automationImportNow.Add(time.Hour))
		return err
	}))
	return result
}

func TestAutomationTranslationImportCompletesHistoryHoldsWorkAndPreservesNativeEdits(t *testing.T) {
	translated := map[string]any{"status": "translated", "source_language": "ja", "translated_text": "Retained translation", "provider": "translate-shell/bing"}
	english := map[string]any{"status": "english", "source_language": "en", "translated_text": "Older rewrite", "provider": "translate-shell/bing"}
	noText := map[string]any{"status": "no-text", "source_language": nil, "translated_text": nil, "provider": nil}
	pending := automationJob(t, "Pending original", "pending", nil)
	retry := automationJob(t, "Retry original", "retry", nil)
	retry["priority"], retry["attempts"] = 100, 10
	done := automationJob(t, "原文 🌿", "done", translated)
	unchanged := automationJob(t, "  Exact English <b>original</b>\n", "done", english)
	empty := automationJob(t, "<p> </p>", "done", noText)
	preserved := automationJob(t, "Prior external translation", "preserved", nil)
	broken := automationJob(t, "Broken cached result", "done", nil)
	broken["result_json"] = "{broken"
	cached := automationJob(t, "Captured but unapplied", "pending", translated)
	dangling := map[string]any{"job_key": strings.Repeat("0", 64)}
	targets := []map[string]any{automationTarget(pending, "post", "caption", 0), automationTarget(retry, "post", "title", 0),
		automationTarget(retry, "other", "caption", 0), automationTarget(done, "post", "title", 1), automationTarget(done, "missing", "caption", 1),
		automationTarget(unchanged, "post", "caption", 1), automationTarget(empty, "post", "title", 1), automationTarget(preserved, "post", "title", 1),
		automationTarget(broken, "post", "caption", 1), automationTarget(dangling, "post", "caption", 0), automationTarget(done, "forgotten", "title", 1),
		automationTarget(cached, "post", "caption", 0)}
	f := translationAutomationFixture(t, []map[string]any{pending, retry, done, unchanged, empty, preserved, broken, cached}, targets)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "galleries", "performers", "performer_names", "metadata_field_decisions"} {
		before[table] = albumJobRows(t, raw, table)
	}
	var collection string
	require.NoError(t, raw.QueryRow("SELECT collection_uuid FROM catalog_collection_mappings WHERE source_uuid=? AND catalog_id=?", f.manifest.SourceUUID, automationCatalog).Scan(&collection))
	ref, err := scrape.CatalogLocalPostReference(f.manifest.SourceUUID, automationCatalog, "post")
	require.NoError(t, err)
	post := sourceTestPost(t, f.repo, ref, "")
	request := retainTranslationRequest(t, f.repo, retry["original_text"].(string))
	revision := 1
	prior := retainTranslationTarget(t, f.repo, models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: post.UUID, CollectionUUID: &collection, CollectionRevision: &revision, Field: "title", Origin: "review"},
		models.TranslationTargetSchedule{State: "held", Priority: 7, NotBefore: automationImportNow.Add(10 * time.Hour)}, automationImportNow)
	ref, err = scrape.CatalogLocalPostReference(f.manifest.SourceUUID, automationCatalog, "forgotten")
	require.NoError(t, err)
	forgotten := sourceTestPost(t, f.repo, ref, "")
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", forgotten.UUID)
	require.NoError(t, err)
	request = retainTranslationRequest(t, f.repo, done["original_text"].(string))
	retainTranslationCache(t, f.repo, models.TranslationCacheInput{RequestUUID: request.UUID, Status: "translated", TranslatedText: translationPointer("Retained translation"),
		SourceLanguage: translationPointer("ja"), Provider: translationPointer("translate-shell/bing"), Origin: "review", CapturedAt: "2026-09-30T00:00:00Z"})
	result := advanceAutomationTranslations(t, f, 0)
	require.Equal(t, "review", result.State)
	require.EqualValues(t, 20, result.ProcessedRecords)
	require.EqualValues(t, 14, result.MappedRecords)
	require.EqualValues(t, 6, result.ReviewRecords)
	require.False(t, result.Imported)
	require.Equal(t, result, advanceAutomationTranslations(t, f, result.LastOrdinal))
	require.EqualValues(t, 7, queryUint(t, raw, "SELECT count(*) FROM translation_requests"))
	require.EqualValues(t, 4, queryUint(t, raw, "SELECT count(*) FROM translation_cache"))
	require.EqualValues(t, 4, queryUint(t, raw, "SELECT count(*) FROM translation_targets WHERE state='held'"))
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM translation_targets WHERE state='completed'"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_translation_evidence WHERE origin='migration' AND captured_at=''"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM translation_targets WHERE state='pending'"))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := f.repo.TranslationWork.Target(ctx, prior.UUID)
		require.NoError(t, err)
		require.Equal(t, prior, current)
		records, err := f.repo.AutomationTranslationImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, records, 20)
		for _, record := range records {
			if record.Disposition == "english_original" {
				details, err := f.repo.AutomationTranslationImport.Record(ctx, f.manifest.UUID, record.Ordinal)
				require.NoError(t, err)
				require.Contains(t, string(details.SourceValues), "Older rewrite")
				cache, err := f.repo.TranslationWork.Cache(ctx, *record.RequestUUID)
				require.NoError(t, err)
				translated, err := f.repo.SourceTranslation.Find(ctx, *cache.TranslationUUID)
				require.NoError(t, err)
				require.Equal(t, unchanged["original_text"], translated.TranslatedText)
			}
		}
		return nil
	}))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.TranslationWork.ScheduleTarget(ctx, prior.UUID, prior.Revision, models.TranslationTargetSchedule{State: "held", Priority: 9, NotBefore: prior.NotBefore}, automationImportNow.Add(2*time.Hour))
		return err
	}))
	require.Equal(t, result, advanceAutomationTranslations(t, f, result.LastOrdinal))
	for table, values := range before {
		require.Equal(t, values, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, raw.Close())
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, result, advanceAutomationTranslations(t, f, result.LastOrdinal))
	backup := filepath.Join(t.TempDir(), "automation-translations-backup.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(backup))
	f.repo = f.db.Repository()
	require.Equal(t, result, advanceAutomationTranslations(t, f, result.LastOrdinal))
	raw = openRawDB(t, backup)
	defer raw.Close()
	require.EqualValues(t, 20, queryUint(t, raw, "SELECT count(*) FROM automation_snapshot_records"))
	require.EqualValues(t, 20, queryUint(t, raw, "SELECT count(*) FROM automation_translation_records"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_translation_evidence WHERE origin='migration' AND captured_at=''"))
}

func TestAutomationTranslationImportBoundedResumeAndEmptyInput(t *testing.T) {
	for _, count := range []int{0, 205} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			jobs := []map[string]any{}
			for i := 0; i < count; i++ {
				jobs = append(jobs, automationJob(t, fmt.Sprintf("Original %d", i), "pending", nil))
			}
			f := translationAutomationFixture(t, jobs, nil)
			first := advanceAutomationTranslations(t, f, 0)
			require.EqualValues(t, min(200, count), first.ProcessedRecords)
			if count > 200 {
				require.Equal(t, "running", first.State)
				require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					_, err := f.repo.AutomationTranslationImport.Advance(ctx, f.manifest.UUID, f.sha, 0, automationImportNow.Add(time.Hour))
					return err
				}), models.ErrAutomationSnapshotConflict)
			}
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			last := advanceAutomationTranslations(t, f, first.LastOrdinal)
			require.Equal(t, "mapped", last.State)
			require.EqualValues(t, count, last.ProcessedRecords)
			require.False(t, last.Imported)
			require.Equal(t, last, advanceAutomationTranslations(t, f, last.LastOrdinal))
		})
	}
}

func TestAutomationTranslationImportCoalescesAliasesWithoutOverridingNativeEdits(t *testing.T) {
	for _, edited := range []bool{false, true} {
		t.Run(fmt.Sprintf("edited=%t", edited), func(t *testing.T) {
			job := automationJob(t, "Shared original", "done", map[string]any{"status": "translated", "source_language": "fr", "translated_text": "Retained translation", "provider": "translate-shell/bing"})
			jobs := []map[string]any{job}
			for i := 0; i < 198; i++ {
				jobs = append(jobs, automationJob(t, fmt.Sprintf("Other original %d", i), "pending", nil))
			}
			// The second key is initially missing, then explicitly linked as an alias.
			// The 200-record boundary separates the hold from historical completion.
			f := translationAutomationFixture(t, jobs, []map[string]any{
				automationTarget(job, "a", "title", 0), automationTarget(job, "missing", "title", 1),
			})
			ref, err := scrape.CatalogLocalPostReference(f.manifest.SourceUUID, automationCatalog, "a")
			require.NoError(t, err)
			post := sourceTestPost(t, f.repo, ref, "")
			alias, err := scrape.CatalogLocalPostReference(f.manifest.SourceUUID, automationCatalog, "missing")
			require.NoError(t, err)
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				return f.repo.SourceEvidence.AddPostIdentifier(ctx, post.UUID, alias, post.Revision)
			}))
			first := advanceAutomationTranslations(t, f, 0)
			require.Equal(t, "running", first.State)
			require.EqualValues(t, 200, first.ProcessedRecords)
			var held *models.TranslationTarget
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				record, err := f.repo.AutomationTranslationImport.Record(ctx, f.manifest.UUID, first.LastOrdinal)
				require.NoError(t, err)
				require.Equal(t, "held", record.Disposition)
				held, err = f.repo.TranslationWork.Target(ctx, *record.TargetUUID)
				return err
			}))
			if edited {
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					_, err := f.repo.TranslationWork.ScheduleTarget(ctx, held.UUID, held.Revision,
						models.TranslationTargetSchedule{State: "held", Priority: 99, NotBefore: held.NotBefore}, automationImportNow.Add(2*time.Hour))
					return err
				}))
			}
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			var result *models.AutomationTranslationImport
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				result, err = f.repo.AutomationTranslationImport.Advance(ctx, f.manifest.UUID, f.sha, first.LastOrdinal, automationImportNow.Add(3*time.Hour))
				return err
			}))
			require.Equal(t, "mapped", result.State)
			require.EqualValues(t, 201, result.ProcessedRecords)
			require.Zero(t, result.ReviewRecords)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				original, err := f.repo.AutomationTranslationImport.Record(ctx, f.manifest.UUID, first.LastOrdinal)
				require.NoError(t, err)
				completed, err := f.repo.AutomationTranslationImport.Record(ctx, f.manifest.UUID, result.LastOrdinal)
				require.NoError(t, err)
				require.Equal(t, original.TargetUUID, completed.TargetUUID)
				require.Equal(t, "held", original.Disposition)
				require.Equal(t, 1, *original.TargetRevision)
				require.Equal(t, 2, *completed.TargetRevision)
				current, err := f.repo.TranslationWork.Target(ctx, held.UUID)
				require.NoError(t, err)
				if edited {
					require.Equal(t, "preserved", completed.Disposition)
					require.Equal(t, "held", current.State)
					require.Equal(t, 99, current.Priority)
					require.Nil(t, current.EvidenceUUID)
				} else {
					require.Equal(t, "completed", completed.Disposition)
					require.Equal(t, "completed", current.State)
					require.Equal(t, held.Priority, current.Priority)
					require.NotNil(t, current.EvidenceUUID)
				}
				return nil
			}))
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			require.Equal(t, result, advanceAutomationTranslations(t, f, result.LastOrdinal))
		})
	}
}

func TestAutomationTranslationImportFailureRollsBackDomainWrites(t *testing.T) {
	job := automationJob(t, "Original", "done", map[string]any{"status": "translated", "source_language": "fr", "translated_text": "Result", "provider": "translate-shell/bing"})
	f := translationAutomationFixture(t, []map[string]any{job}, []map[string]any{automationTarget(job, "post", "title", 1)})
	attachmentSQL(t, f.db, `CREATE TRIGGER reject_automation_translation BEFORE INSERT ON automation_translation_records WHEN NEW.disposition='completed'
 BEGIN SELECT RAISE(ABORT,'late historical completion failure'); END`)
	require.ErrorContains(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.AutomationTranslationImport.Advance(ctx, f.manifest.UUID, f.sha, 0, automationImportNow.Add(time.Hour))
		require.ErrorContains(t, err, "late historical completion failure")
		return nil
	}), "snapshot write did not finish atomically")
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	for _, table := range []string{"automation_translation_imports", "automation_translation_records", "translation_requests", "translation_cache", "translation_targets", "translation_target_history", "source_translation_evidence", "source_translations"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_posts WHERE revision!=1"))
	attachmentSQL(t, f.db, "DROP TRIGGER reject_automation_translation")
	result := advanceAutomationTranslations(t, f, 0)
	require.Equal(t, "mapped", result.State)
	require.EqualValues(t, 2, result.MappedRecords)
	query := `EXPLAIN QUERY PLAN SELECT ordinal,data,data_sha256 FROM automation_snapshot_records INDEXED BY automation_translation_input
 WHERE snapshot_uuid=? AND ordinal>? AND source_table IN ('translation_jobs','translation_targets') ORDER BY ordinal LIMIT 1`
	rows, err := raw.Query(query, f.manifest.UUID, 0)
	require.NoError(t, err)
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plan = append(plan, detail)
	}
	require.NoError(t, rows.Err())
	require.Contains(t, strings.Join(plan, "\n"), "automation_translation_input")
	require.NotContains(t, strings.Join(plan, "\n"), "TEMP B-TREE")
	require.NoError(t, rows.Close())
	require.NoError(t, raw.Close())
	output := f.db.DatabasePath() + ".anonymous"
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	raw = openRawDB(t, output)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM automation_translation_records"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM automation_translation_imports"))
}

func TestAutomationTranslationImportWaitsForKnownCatalogEvidence(t *testing.T) {
	job := automationJob(t, "Original", "pending", nil)
	f := translationAutomationFixture(t, []map[string]any{job}, []map[string]any{automationTarget(job, "missing", "title", 0)})
	body, err := os.ReadFile("../scrape/testdata/catalog_snapshot/manifest.json")
	require.NoError(t, err)
	var manifest scrape.CatalogSnapshotManifest
	require.NoError(t, json.Unmarshal(body, &manifest))
	manifest.SourceUUID = f.manifest.SourceUUID
	body, err = json.Marshal(manifest)
	require.NoError(t, err)
	cf := &catalogSnapshotFixture{db: f.db, repo: f.repo, body: body, sha: scrape.CatalogSnapshotSHA(body), manifest: &manifest}
	cf.begin(t)
	assertWaiting := func() {
		require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.AutomationTranslationImport.Advance(ctx, f.manifest.UUID, f.sha, 0, automationImportNow.Add(time.Hour))
			return err
		}), models.ErrAutomationSnapshotConflict)
		raw := openRawDB(t, f.db.DatabasePath())
		defer raw.Close()
		for _, table := range []string{"automation_translation_imports", "automation_translation_records", "translation_requests", "translation_targets"} {
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
		}
	}
	assertWaiting()
	for index := range manifest.Chunks {
		cf.receive(t, index)
	}
	assertWaiting()
	evidence := advanceEvidence(t, cf, 0)
	for evidence.State == "running" {
		evidence = advanceEvidence(t, cf, evidence.LastOrdinal)
	}
	result := advanceAutomationTranslations(t, f, 0)
	require.Equal(t, "review", result.State)
	require.EqualValues(t, 1, result.ReviewRecords)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		record, err := f.repo.AutomationTranslationImport.Record(ctx, f.manifest.UUID, result.LastOrdinal)
		require.NoError(t, err)
		require.Equal(t, "legacy_post_unmatched", record.Reason)
		return nil
	}))
}

func TestAutomationTranslationImportPreservesConflictingNativeCache(t *testing.T) {
	job := automationJob(t, "Original", "done", map[string]any{"status": "translated", "source_language": "fr", "translated_text": "Legacy result", "provider": "translate-shell/bing"})
	f := translationAutomationFixture(t, []map[string]any{job}, []map[string]any{automationTarget(job, "post", "title", 1)})
	request := retainTranslationRequest(t, f.repo, "Original")
	prior := retainTranslationCache(t, f.repo, models.TranslationCacheInput{RequestUUID: request.UUID, Status: "translated", TranslatedText: translationPointer("Native choice"), Origin: "review"})
	result := advanceAutomationTranslations(t, f, 0)
	require.EqualValues(t, 2, result.ReviewRecords)
	require.Zero(t, result.MappedRecords)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		cache, err := f.repo.TranslationWork.Cache(ctx, request.UUID)
		require.NoError(t, err)
		require.Equal(t, prior, cache)
		rows, err := f.repo.AutomationTranslationImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		require.Equal(t, "native_cache_conflict", rows[0].Reason)
		require.Equal(t, "legacy_job_requires_review", rows[1].Reason)
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestAutomationTranslationImportRefusesCorruptReceiptsBeforeWrites(t *testing.T) {
	for _, mutation := range []struct{ name, trigger, sql string }{
		{"checkpoint", "automation_translation_import_guard", "UPDATE automation_translation_imports SET last_ordinal=last_ordinal+1"},
		{"post scope", "automation_translation_record_immutable", "UPDATE automation_translation_records SET post_reference='post:'||lower(hex(zeroblob(32))) WHERE post_uuid IS NOT NULL"},
		{"completion receipt", "", "DELETE FROM automation_translation_records WHERE disposition='completed'"},
		{"source job", "automation_translation_record_immutable", "UPDATE automation_translation_records SET disposition='request',cache_uuid=NULL WHERE disposition='cache'"},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			job := automationJob(t, "Original", "done", map[string]any{"status": "translated", "source_language": "fr", "translated_text": "Result", "provider": "translate-shell/bing"})
			f := translationAutomationFixture(t, []map[string]any{job}, []map[string]any{automationTarget(job, "post", "title", 1)})
			advanceAutomationTranslations(t, f, 0)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			command := mutation.sql
			if mutation.trigger != "" {
				var guard string
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", mutation.trigger).Scan(&guard))
				command = "DROP TRIGGER " + mutation.trigger + ";" + command + ";" + guard
			}
			_, err := raw.Exec(command)
			require.NoError(t, err)
			require.NoError(t, raw.Close())
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			check := sqlite.NewDatabase()
			require.Error(t, check.Open(path))
			require.NoError(t, check.Close())
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
