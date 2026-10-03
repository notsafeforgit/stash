package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeCatalogEnrichmentSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeAutomationEnrichmentSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER source_enrichment_receipt_immutable; DROP TRIGGER source_enrichment_receipt_scope;
 DROP TRIGGER catalog_enrichment_import_guard; DROP TRIGGER catalog_enrichment_record_immutable;
 DROP TABLE catalog_enrichment_records; DROP TABLE catalog_enrichment_imports; DROP TABLE source_enrichment_receipts;
 DELETE FROM native_migration_history WHERE version=1000057;`)
	require.NoError(t, err)
}

func enrichmentImportFixture(t *testing.T, count int, alter func(int, map[string]any)) *catalogSnapshotFixture {
	t.Helper()
	f := snapshotFixture(t)
	f.manifest.Tables["enrichment_receipts"] = scrape.CatalogSnapshotTable{Key: []string{"post_key", "version"}, Columns: []scrape.CatalogSnapshotColumn{
		{CID: 0, Name: "post_key", Type: "TEXT", NotNull: 1, PK: 1, Default: json.RawMessage(`null`)},
		{CID: 1, Name: "version", Type: "INTEGER", NotNull: 1, PK: 2, Default: json.RawMessage(`null`)},
		{CID: 2, Name: "completed_at", Type: "TEXT", NotNull: 1, Default: json.RawMessage(`null`)},
		{CID: 3, Name: "details_json", Type: "TEXT", NotNull: 1, Default: json.RawMessage(`null`)},
	}}
	entry := f.manifest.Schema[0]
	entry.Type, entry.Name, entry.Table = "table", "enrichment_receipts", "enrichment_receipts"
	entry.SQL = "CREATE TABLE enrichment_receipts(post_key TEXT NOT NULL,version INTEGER NOT NULL,completed_at TEXT NOT NULL,details_json TEXT NOT NULL,PRIMARY KEY(post_key,version))"
	f.manifest.Schema = append(f.manifest.Schema, entry)
	f.manifest.References["enrichment_receipts.post_key"] = 0
	captured, err := time.Parse(time.RFC3339Nano, f.manifest.CapturedAt)
	require.NoError(t, err)
	completed := captured.Add(-time.Minute).In(time.FixedZone("historical", -7*60*60)).Format(time.RFC3339Nano)
	rows := documentImportRows(t, f)
	var template map[string]any
	for _, row := range rows {
		if row["table"] == "posts" {
			template = row["values"].(map[string]any)
		}
	}
	require.NotNil(t, template)
	for i := range count {
		key := fmt.Sprintf("reddit:post:receipt%03d", i)
		post := maps.Clone(template)
		post["post_key"] = key
		post["source_id"] = fmt.Sprintf("receipt%03d", i)
		rows = append(rows, map[string]any{"table": "posts", "key": []any{key}, "values": post})
		v := map[string]any{"post_key": key, "version": 1, "completed_at": completed,
			"details_json": `{"attachment_links_enriched":3,"unresolved_children":2}`}
		if alter != nil {
			alter(i, v)
		}
		rows = append(rows, map[string]any{"table": "enrichment_receipts", "key": []any{v["post_key"], v["version"]}, "values": v})
	}
	return receiveCatalogFixtureRows(t, f, rows)
}

func advanceCatalogEnrichment(t *testing.T, f *catalogSnapshotFixture, after int64) *models.CatalogEnrichmentImport {
	t.Helper()
	var result *models.CatalogEnrichmentImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.CatalogEnrichmentImport.Advance(ctx, f.manifest.UUID, f.sha, after, catalogImportNow.Add(7*time.Hour))
		return err
	}))
	return result
}

func TestCatalogEnrichmentImportResumesAndPreservesHistory(t *testing.T) {
	f := enrichmentImportFixture(t, 55, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "galleries", "performers", "source_posts", "source_captures", "source_post_revisions", "enrichment_targets", "enrichment_completions", "enrichment_job_attempts", "archive_jobs"} {
		before[table] = albumJobRows(t, raw, table)
	}
	first := advanceCatalogEnrichment(t, f, 0)
	require.Equal(t, "running", first.State)
	require.EqualValues(t, 50, first.ProcessedRecords)
	for _, attempt := range []struct {
		after int64
		now   time.Time
	}{{0, catalogImportNow}, {first.LastOrdinal, catalogImportNow}} {
		err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.CatalogEnrichmentImport.Advance(ctx, f.manifest.UUID, f.sha, attempt.after, attempt.now)
			return err
		})
		require.ErrorIs(t, err, models.ErrCatalogSnapshotConflict)
	}
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: first.CollectionUUID, ExpectedRevision: 1, Origin: "review",
		SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Later collection", Kind: "directory", State: "retired"}})
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	result := advanceCatalogEnrichment(t, f, first.LastOrdinal)
	require.Equal(t, "mapped", result.State)
	require.EqualValues(t, 55, result.MappedRecords)
	require.Equal(t, 1, result.CollectionRevision)
	require.False(t, result.Imported)
	require.Equal(t, result, advanceCatalogEnrichment(t, f, result.LastOrdinal))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogEnrichmentImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, rows, 55)
		for _, row := range rows {
			detail, err := f.repo.CatalogEnrichmentImport.Record(ctx, f.manifest.UUID, row.Ordinal)
			require.NoError(t, err)
			var values map[string]any
			require.NoError(t, json.Unmarshal(detail.SourceValues, &values))
			receipt, err := f.repo.SourceEnrichmentReceipt.Find(ctx, *row.ReceiptUUID)
			require.NoError(t, err)
			require.Equal(t, values["completed_at"], receipt.CompletedAt)
			require.EqualValues(t, 3, receipt.AttachmentLinksEnriched)
			require.EqualValues(t, 2, receipt.UnresolvedChildren, "a legacy completion does not certify exhaustive child coverage")
			require.Equal(t, "migration", receipt.Origin)
			page, err := f.repo.SourceEnrichmentReceipt.PostReceipts(ctx, *row.PostUUID, "", 1)
			require.NoError(t, err)
			require.Equal(t, []models.SourceEnrichmentReceipt{*receipt}, page)
			page, err = f.repo.SourceEnrichmentReceipt.PostReceipts(ctx, *row.PostUUID, receipt.UUID, 1)
			require.NoError(t, err)
			require.Empty(t, page)
		}
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	check := openRawDB(t, output)
	defer check.Close()
	for _, table := range []string{"source_enrichment_receipts", "catalog_enrichment_imports", "catalog_enrichment_records"} {
		require.Zero(t, queryUint(t, check, "SELECT count(*) FROM "+table))
	}
}

func TestCatalogEnrichmentImportReviewsInvalidAndUnmatchedReceipts(t *testing.T) {
	f := enrichmentImportFixture(t, 5, func(i int, v map[string]any) {
		switch i {
		case 1:
			v["version"] = 2
		case 2:
			v["details_json"] = `{"attachment_links_enriched":-1,"unresolved_children":0}`
		case 3:
			v["post_key"] = "reddit:post:absent"
		case 4:
			v["completed_at"] = "2027-01-01T00:00:00Z"
		}
	})
	result := advanceCatalogEnrichment(t, f, 0)
	require.EqualValues(t, 1, result.MappedRecords)
	require.EqualValues(t, 4, result.ReviewRecords)
	require.Equal(t, "review", result.State)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogEnrichmentImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		for _, row := range rows {
			if row.Outcome == "review" {
				require.Nil(t, row.ReceiptUUID)
				require.NotEmpty(t, row.Reason)
			}
		}
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestCatalogEnrichmentImportAtomicRollbackAndForgottenPost(t *testing.T) {
	f := enrichmentImportFixture(t, 1, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	attachmentSQL(t, f.db, "CREATE TRIGGER reject_enrichment_record BEFORE INSERT ON catalog_enrichment_records BEGIN SELECT RAISE(ABORT,'receipt failure'); END")
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogEnrichmentImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow.Add(7*time.Hour))
		require.ErrorContains(t, err, "receipt failure")
		return nil
	})
	require.ErrorContains(t, err, "did not finish atomically")
	for _, table := range []string{"source_enrichment_receipts", "catalog_enrichment_imports", "catalog_enrichment_records"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	attachmentSQL(t, f.db, "DROP TRIGGER reject_enrichment_record")
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten'")
	require.NoError(t, err)
	result := advanceCatalogEnrichment(t, f, 0)
	require.EqualValues(t, 1, result.ReviewRecords)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_enrichment_receipts"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestCatalogEnrichmentMigrationPreservesExistingState(t *testing.T) {
	f := enrichmentImportFixture(t, 1, nil)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeCatalogEnrichmentSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000056")
	require.NoError(t, err)
	before := map[string][][]any{}
	for _, table := range []string{"catalog_snapshots", "catalog_snapshot_records", "source_posts", "source_captures", "source_collections", "source_service_turns"} {
		before[table] = albumJobRows(t, raw, table)
	}
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(f.db.Open(path), &needed))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.EqualValues(t, 1, advanceCatalogEnrichment(t, f, 0).MappedRecords)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestCatalogEnrichmentStartupRejectsReceiptTampering(t *testing.T) {
	for _, test := range []struct{ name, change, message string }{
		{"counts", "unresolved_children=0", "invalid catalog enrichment receipt assertion"},
		{"recording_time", "recorded_at='2020-01-01T00:00:00Z'", "invalid catalog enrichment receipt recording time"},
	} {
		t.Run(test.name, func(t *testing.T) {

			f := enrichmentImportFixture(t, 1, nil)
			advanceCatalogEnrichment(t, f, 0)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			defer raw.Close()
			var trigger string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='source_enrichment_receipt_immutable'").Scan(&trigger))
			_, err := raw.Exec("DROP TRIGGER source_enrichment_receipt_immutable; UPDATE source_enrichment_receipts SET " + test.change + "; " + trigger)
			require.NoError(t, err)
			require.ErrorContains(t, f.db.Open(path), test.message)
		})
	}
}

func TestCatalogEnrichmentMigrationCollisionRollsBack(t *testing.T) {
	f := enrichmentImportFixture(t, 1, nil)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeCatalogEnrichmentSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000056")
	require.NoError(t, err)
	before := albumJobRows(t, raw, "catalog_snapshot_records")
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(f.db.Open(path), &needed))
	// Collide after some of the migration's DDL has run, proving the earlier
	// table/index creation is rolled back as well as the source being intact.
	_, err = raw.Exec("CREATE TABLE catalog_enrichment_imports(original TEXT); INSERT INTO catalog_enrichment_imports VALUES('retained')")
	require.NoError(t, err)
	require.Error(t, f.db.RunAllMigrations())
	var original string
	require.NoError(t, raw.QueryRow("SELECT original FROM catalog_enrichment_imports").Scan(&original))
	require.Equal(t, "retained", original)
	require.Equal(t, before, albumJobRows(t, raw, "catalog_snapshot_records"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='source_enrichment_receipts'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000057"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty=1"))
}
