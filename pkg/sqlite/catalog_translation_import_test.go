package sqlite_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func translationImportFixture(t *testing.T, count int, alter func(int, map[string]any)) *catalogSnapshotFixture {
	t.Helper()
	f := snapshotFixture(t)
	rows := documentImportRows(t, f)
	original := "原文\u2028\\u2028\n<&>🌿"
	body, err := scrape.LegacyCatalogJSON(original, 4096)
	require.NoError(t, err)
	for i := range count {
		v := map[string]any{"translation_id": fmt.Sprintf("translation-%03d", i), "post_key": "reddit:post:album", "input_hash": scrape.CatalogSnapshotSHA(body),
			"original_text": original, "translated_text": "Historical translation", "source_language": nil, "target_language": nil, "provider": nil,
			"provenance": fmt.Sprintf("retained:%d", i), "captured_at": "2026-09-30T01:02:03.123456789-07:00"}
		if alter != nil {
			alter(i, v)
		}
		rows = append(rows, map[string]any{"table": "translations", "key": []any{v["translation_id"]}, "values": v})
	}
	return receiveCatalogFixtureRows(t, f, rows)
}

func advanceTranslations(t *testing.T, f *catalogSnapshotFixture, after int64) *models.CatalogTranslationImport {
	t.Helper()
	var ret *models.CatalogTranslationImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = f.repo.CatalogTranslationImport.Advance(ctx, f.manifest.UUID, f.sha, after, catalogImportNow.Add(7*time.Hour))
		return err
	}))
	return ret
}

func TestCatalogTranslationImportResumesSharesResultsAndPreservesScope(t *testing.T) {
	f := translationImportFixture(t, 55, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "galleries", "performers", "metadata_field_decisions", "source_captures", "source_post_revisions"} {
		before[table] = albumJobRows(t, raw, table)
	}
	initial := queryUint(t, raw, "SELECT sum(revision) FROM source_posts")
	first := advanceTranslations(t, f, 0)
	require.Equal(t, "running", first.State)
	require.EqualValues(t, 50, first.ProcessedRecords)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogTranslationImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		return err
	})
	require.ErrorIs(t, err, models.ErrCatalogSnapshotConflict)
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: first.CollectionUUID, ExpectedRevision: 1, Origin: "review",
		SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Later collection", Kind: "directory", State: "retired"}})
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	result := advanceTranslations(t, f, first.LastOrdinal)
	require.Equal(t, "mapped", result.State)
	require.Equal(t, 1, result.CollectionRevision)
	require.EqualValues(t, 55, result.MappedRecords)
	require.False(t, result.Imported)
	require.Equal(t, result, advanceTranslations(t, f, result.LastOrdinal))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_translations"))
	require.EqualValues(t, 55, queryUint(t, raw, "SELECT count(*) FROM source_translation_evidence"))
	require.Equal(t, initial+55, queryUint(t, raw, "SELECT sum(revision) FROM source_posts"))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogTranslationImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, rows, 55)
		for _, row := range rows {
			require.Equal(t, "verified", row.InputHashState)
			detail, err := f.repo.CatalogTranslationImport.Record(ctx, f.manifest.UUID, row.Ordinal)
			require.NoError(t, err)
			var values map[string]any
			require.NoError(t, json.Unmarshal(detail.SourceValues, &values))
			evidence, err := f.repo.SourceTranslation.Evidence(ctx, *row.EvidenceUUID)
			require.NoError(t, err)
			require.Equal(t, values["captured_at"], evidence.CapturedAt)
			require.Equal(t, values["provenance"], evidence.Provenance)
			require.Equal(t, values["input_hash"], *evidence.DeclaredInputHash)
			translation, err := f.repo.SourceTranslation.Find(ctx, *row.TranslationUUID)
			require.NoError(t, err)
			require.Nil(t, translation.TargetLanguage)
			require.Nil(t, translation.Provider)
			require.Equal(t, values["original_text"], *translation.OriginalText)
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
	for _, table := range []string{"source_translations", "source_translation_evidence", "catalog_translation_imports", "catalog_translation_records"} {
		require.Zero(t, queryUint(t, check, "SELECT count(*) FROM "+table))
	}
}

func TestCatalogTranslationImportRetainsMissingOrConflictingIdentityAndInvalidTime(t *testing.T) {
	f := translationImportFixture(t, 6, func(i int, v map[string]any) {
		switch i {
		case 1:
			v["input_hash"] = strings.Repeat("0", 64)
		case 2:
			v["original_text"] = nil
		case 3:
			v["input_hash"] = nil
		case 4:
			v["captured_at"] = "unknown historical time"
		case 5:
			v["source_language"], v["target_language"] = "ja", "en"
		}
	})
	result := advanceTranslations(t, f, 0)
	require.Equal(t, "review", result.State)
	require.EqualValues(t, 2, result.ReviewRecords)
	require.EqualValues(t, 4, result.MappedRecords)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogTranslationImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		require.Equal(t, "mismatch", rows[1].InputHashState)
		require.NotNil(t, rows[1].EvidenceUUID, "the conflicting assertion is retained without becoming a verified hash")
		require.Equal(t, "unverifiable", rows[2].InputHashState)
		require.Equal(t, "missing", rows[3].InputHashState)
		require.Equal(t, "invalid_translation_provenance", rows[4].Reason)
		require.Nil(t, rows[4].EvidenceUUID)
		require.NotNil(t, rows[4].TranslationUUID)
		english, err := f.repo.SourceTranslation.PostEvidence(ctx, models.SourceTranslationQuery{PostUUID: *rows[0].PostUUID, TargetLanguage: translationPointer("en"), Limit: 100})
		require.NoError(t, err)
		require.Len(t, english, 1)
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestCatalogTranslationImportAtomicRollbackAndForgottenPost(t *testing.T) {
	f := translationImportFixture(t, 1, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	initial := queryUint(t, raw, "SELECT sum(revision) FROM source_posts")
	attachmentSQL(t, f.db, "CREATE TRIGGER reject_translation_receipt BEFORE INSERT ON catalog_translation_records BEGIN SELECT RAISE(ABORT,'receipt failure'); END")
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogTranslationImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		require.ErrorContains(t, err, "receipt failure")
		return nil
	})
	require.ErrorContains(t, err, "did not finish atomically")
	for _, table := range []string{"source_translations", "source_translation_evidence", "catalog_translation_imports", "catalog_translation_records"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Equal(t, initial, queryUint(t, raw, "SELECT sum(revision) FROM source_posts"))
	attachmentSQL(t, f.db, "DROP TRIGGER reject_translation_receipt")
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten'")
	require.NoError(t, err)
	result := advanceTranslations(t, f, 0)
	require.Equal(t, "review", result.State)
	require.EqualValues(t, 1, result.ReviewRecords)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_translation_evidence"))
	require.Equal(t, initial, queryUint(t, raw, "SELECT sum(revision) FROM source_posts"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}
