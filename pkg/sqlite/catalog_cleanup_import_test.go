package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeCatalogCleanupSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removePostMediaDecisionSchema(t, raw)
	_, err := raw.Exec(`DROP TRIGGER source_cleanup_intent_immutable; DROP TRIGGER source_cleanup_intent_scope;
 DROP TRIGGER catalog_cleanup_import_guard; DROP TRIGGER catalog_cleanup_record_immutable;
 DROP TABLE catalog_cleanup_records; DROP TABLE catalog_cleanup_imports; DROP TABLE source_cleanup_intents;
 DELETE FROM native_migration_history WHERE version=1000080;`)
	require.NoError(t, err)
}

func cleanupImportFixture(t *testing.T, count int, alter func(int, map[string]any)) *catalogSnapshotFixture {
	t.Helper()
	f := snapshotFixture(t)
	f.manifest.Tables["metadata_prune_queue"] = scrape.CatalogSnapshotTable{Key: []string{"post_key"}, Columns: []scrape.CatalogSnapshotColumn{
		{CID: 0, Name: "post_key", Type: "TEXT", PK: 1, Default: json.RawMessage(`null`)},
		{CID: 1, Name: "pruned_at", Type: "TEXT", NotNull: 1, Default: json.RawMessage(`null`)},
	}}
	entry := f.manifest.Schema[0]
	entry.Type, entry.Name, entry.Table = "table", "metadata_prune_queue", "metadata_prune_queue"
	entry.SQL = "CREATE TABLE metadata_prune_queue(post_key TEXT PRIMARY KEY,pruned_at TEXT NOT NULL)"
	f.manifest.Schema = append(f.manifest.Schema, entry)
	captured, err := time.Parse(time.RFC3339Nano, f.manifest.CapturedAt)
	require.NoError(t, err)
	stamp := captured.Add(-time.Minute).In(time.FixedZone("historical", -7*60*60)).Format(time.RFC3339Nano)
	rows := documentImportRows(t, f)
	for i := range count {
		values := map[string]any{"post_key": fmt.Sprintf("reddit:post:removed%03d", i), "pruned_at": stamp}
		if alter != nil {
			alter(i, values)
		}
		rows = append(rows, map[string]any{"table": "metadata_prune_queue", "key": []any{values["post_key"]}, "values": values})
	}
	// A queued alias may have been recreated before the frozen boundary. It
	// must be retained independently from the source post or its current work.
	f.manifest.Tables["post_aliases"] = scrape.CatalogSnapshotTable{Key: []string{"alias_key"}, Columns: []scrape.CatalogSnapshotColumn{
		{CID: 0, Name: "alias_key", Type: "TEXT", PK: 1, Default: json.RawMessage(`null`)},
		{CID: 1, Name: "post_key", Type: "TEXT", NotNull: 1, Default: json.RawMessage(`null`)},
	}}
	entry.Type, entry.Name, entry.Table = "table", "post_aliases", "post_aliases"
	entry.SQL = "CREATE TABLE post_aliases(alias_key TEXT PRIMARY KEY,post_key TEXT NOT NULL REFERENCES posts(post_key))"
	f.manifest.Schema = append(f.manifest.Schema, entry)
	f.manifest.References["post_aliases.post_key"] = 0
	rows = append(rows, map[string]any{"table": "post_aliases", "key": []any{"reddit:post:recreated-alias"}, "values": map[string]any{"alias_key": "reddit:post:recreated-alias", "post_key": "reddit:post:album"}})
	return receiveCatalogFixtureRows(t, f, rows)
}

func advanceCatalogCleanup(t *testing.T, f *catalogSnapshotFixture, after int64) *models.CatalogCleanupImport {
	t.Helper()
	var result *models.CatalogCleanupImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.CatalogCleanupImport.Advance(ctx, f.manifest.UUID, f.sha, after, catalogImportNow.Add(7*time.Hour))
		return err
	}))
	return result
}

func TestCatalogCleanupResumesWithoutDeletingRecreatedPostsOrSharedWork(t *testing.T) {
	f := cleanupImportFixture(t, 55, func(i int, values map[string]any) {
		switch i {
		case 0:
			values["post_key"] = "reddit:post:album"
		case 1:
			values["post_key"] = "reddit:post:recreated-alias"
		}
	})
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var existingPost string
	require.NoError(t, raw.QueryRow("SELECT uuid FROM source_posts LIMIT 1").Scan(&existingPost))
	other := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "shared-cleanup-result"}, "")
	request := retainTranslationRequest(t, f.repo, "Original shared text")
	for _, post := range []string{existingPost, other.UUID} {
		retainTranslationTarget(t, f.repo, models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: post, Field: "title", Origin: "capture"}, models.TranslationTargetSchedule{State: "pending", Priority: 100}, catalogImportNow)
	}
	retainTranslationCache(t, f.repo, models.TranslationCacheInput{RequestUUID: request.UUID, Status: "translated", TranslatedText: translationPointer("Shared translation"), SourceLanguage: translationPointer("ja"), Provider: translationPointer("translate-shell/bing"), CapturedAt: f.manifest.CapturedAt, Origin: "migration"})
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "galleries", "performers", "source_posts", "source_captures", "source_post_revisions", "source_post_identifiers", "translation_requests", "translation_cache", "translation_targets", "translation_target_history", "source_translations", "enrichment_targets", "archive_jobs", "catalog_snapshot_records"} {
		before[table] = albumJobRows(t, raw, table)
	}
	first := advanceCatalogCleanup(t, f, 0)
	require.Equal(t, "running", first.State)
	require.EqualValues(t, 50, first.HeldRecords)
	for _, input := range []struct {
		after int64
		sha   string
		now   time.Time
	}{{0, f.sha, catalogImportNow.Add(8 * time.Hour)}, {first.LastOrdinal, f.sha, catalogImportNow}, {first.LastOrdinal, scrape.CatalogSnapshotSHA([]byte("other")), catalogImportNow.Add(8 * time.Hour)}} {
		err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.CatalogCleanupImport.Advance(ctx, f.manifest.UUID, input.sha, input.after, input.now)
			return err
		})
		require.ErrorIs(t, err, models.ErrCatalogSnapshotConflict)
	}
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: first.CollectionUUID, ExpectedRevision: 1, Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Later collection", Kind: "directory", State: "retired"}})
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	result := advanceCatalogCleanup(t, f, first.LastOrdinal)
	require.Equal(t, "retained", result.State)
	require.EqualValues(t, 55, result.HeldRecords)
	require.Equal(t, 1, result.CollectionRevision)
	require.False(t, result.Imported)
	require.Equal(t, result, advanceCatalogCleanup(t, f, result.LastOrdinal))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogCleanupImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, rows, 55)
		for _, row := range rows {
			detail, err := f.repo.CatalogCleanupImport.Record(ctx, f.manifest.UUID, row.Ordinal)
			require.NoError(t, err)
			var values map[string]any
			require.NoError(t, json.Unmarshal(detail.SourceValues, &values))
			intent, err := f.repo.SourceCleanupIntent.Find(ctx, *row.IntentUUID)
			require.NoError(t, err)
			require.Equal(t, values["post_key"], intent.ReferenceValue)
			require.Equal(t, values["pruned_at"], intent.RequestedAt)
			require.Equal(t, "legacy:catalog:"+f.manifest.SourceUUID+":"+f.manifest.CatalogID, intent.ReferenceNamespace)
			require.Equal(t, "held", intent.State)
			require.Equal(t, "migration", intent.Origin)
		}
		page, err := f.repo.SourceCleanupIntent.CollectionIntents(ctx, first.CollectionUUID, "", 1)
		require.NoError(t, err)
		require.Len(t, page, 1)
		next, err := f.repo.SourceCleanupIntent.CollectionIntents(ctx, first.CollectionUUID, page[0].UUID, 100)
		require.NoError(t, err)
		require.Len(t, next, 54)
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
	for _, table := range []string{"source_cleanup_intents", "catalog_cleanup_imports", "catalog_cleanup_records"} {
		require.Zero(t, queryUint(t, check, "SELECT count(*) FROM "+table))
	}
}

func TestCatalogCleanupRetainsInvalidRowsForReview(t *testing.T) {
	f := cleanupImportFixture(t, 4, func(i int, values map[string]any) {
		switch i {
		case 1:
			values["pruned_at"] = "unknown"
		case 2:
			values["pruned_at"] = "2027-01-01T00:00:00Z"
		case 3:
			values["post_key"] = ""
		}
	})
	result := advanceCatalogCleanup(t, f, 0)
	require.Equal(t, "review", result.State)
	require.EqualValues(t, 1, result.HeldRecords)
	require.EqualValues(t, 3, result.ReviewRecords)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogCleanupImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		for _, row := range rows {
			detail, err := f.repo.CatalogCleanupImport.Record(ctx, f.manifest.UUID, row.Ordinal)
			require.NoError(t, err)
			require.NotEmpty(t, detail.SourceValues)
			if row.Outcome == "review" {
				require.Nil(t, row.IntentUUID)
				require.NotEmpty(t, row.Reason)
			}
		}
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestCatalogCleanupEmptyOrAbsentQueueCompletes(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(fmt.Sprint(absent), func(t *testing.T) {
			var f *catalogSnapshotFixture
			if absent {
				f = snapshotFixture(t)
				f = receiveCatalogFixtureRows(t, f, documentImportRows(t, f))
			} else {
				f = cleanupImportFixture(t, 0, nil)
			}
			result := advanceCatalogCleanup(t, f, 0)
			require.Equal(t, "retained", result.State)
			require.Zero(t, result.TotalRecords)
			require.Zero(t, result.LastOrdinal)
			require.False(t, result.Imported)
			require.Equal(t, result, advanceCatalogCleanup(t, f, 0))
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
		})
	}
}

func TestCatalogCleanupIgnoredReceiptFailureRollsBack(t *testing.T) {
	f := cleanupImportFixture(t, 1, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	attachmentSQL(t, f.db, "CREATE TRIGGER reject_cleanup_record BEFORE INSERT ON catalog_cleanup_records BEGIN SELECT RAISE(ABORT,'receipt failure'); END")
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogCleanupImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow.Add(7*time.Hour))
		require.ErrorContains(t, err, "receipt failure")
		return nil
	})
	require.ErrorContains(t, err, "did not finish atomically")
	for _, table := range []string{"source_cleanup_intents", "catalog_cleanup_imports", "catalog_cleanup_records"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	attachmentSQL(t, f.db, "DROP TRIGGER reject_cleanup_record")
	require.EqualValues(t, 1, advanceCatalogCleanup(t, f, 0).HeldRecords)
}

func TestCatalogCleanupMigrationPreservesExistingState(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			f := cleanupImportFixture(t, 1, nil)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			defer raw.Close()
			removeCatalogCleanupSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000079,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"catalog_snapshot_records", "source_posts", "source_captures", "source_collections", "metadata_policy_imports"} {
				before[table] = albumJobRows(t, raw, table)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(path), &needed))
			if collision {
				_, err = raw.Exec("CREATE TABLE catalog_cleanup_imports(original TEXT); INSERT INTO catalog_cleanup_imports VALUES('retained')")
				require.NoError(t, err)
				require.Error(t, f.db.RunAllMigrations())
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM catalog_cleanup_imports").Scan(&original))
				require.Equal(t, "retained", original)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='source_cleanup_intents'"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000080"))
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
				require.EqualValues(t, 1, advanceCatalogCleanup(t, f, 0).HeldRecords)
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}

func TestCatalogCleanupAuditRejectsChangedIntent(t *testing.T) {
	for _, change := range []string{"reference_value='reddit:post:other'", "requested_at='2020-01-01T00:00:00Z'", "recorded_at='2020-01-01T00:00:00Z'"} {
		t.Run(change, func(t *testing.T) {
			f := cleanupImportFixture(t, 1, nil)
			advanceCatalogCleanup(t, f, 0)
			path := f.db.DatabasePath()
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, path)
			defer raw.Close()
			var trigger string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='source_cleanup_intent_immutable'").Scan(&trigger))
			_, err := raw.Exec("DROP TRIGGER source_cleanup_intent_immutable; UPDATE source_cleanup_intents SET " + change + "; " + trigger)
			require.NoError(t, err)
			before := albumJobRows(t, raw, "source_cleanup_intents")
			require.Error(t, f.db.AuditForTesting(path))
			require.Equal(t, before, albumJobRows(t, raw, "source_cleanup_intents"))
		})
	}
}
