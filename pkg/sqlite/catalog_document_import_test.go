package sqlite_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func documentImportRows(t *testing.T, f *catalogSnapshotFixture) []map[string]any {
	t.Helper()
	var rows []map[string]any
	for i := range f.manifest.Chunks {
		for _, line := range bytes.Split(bytes.TrimSpace(f.chunk(t, i)), []byte("\n")) {
			row, err := archive.DecodeJSONObject(line, scrape.CatalogChunkLimit)
			require.NoError(t, err)
			rows = append(rows, row)
		}
	}
	return rows
}

func documentImportFixture(t *testing.T, extra int, explicit bool, alter func([]map[string]any)) *catalogSnapshotFixture {
	t.Helper()
	f := snapshotFixture(t)
	rows := documentImportRows(t, f)
	var template map[string]any
	for _, row := range rows {
		if row["table"] == "sidecar_sources" {
			template = row["values"].(map[string]any)
		}
	}
	require.NotNil(t, template)
	for i := range extra {
		v := map[string]any{}
		for key, value := range template {
			v[key] = value
		}
		v["relpath"], v["post_key"] = fmt.Sprintf("folder\\literal/%03d.nfo", i), "reddit:post:album"
		rows = append(rows, map[string]any{"table": "sidecar_sources", "key": []any{v["relpath"], v["content_sha256"]}, "values": v})
	}
	if explicit {
		table := scrape.CatalogSnapshotTable{Key: []string{"relpath"}, Columns: []scrape.CatalogSnapshotColumn{
			{CID: 0, Name: "relpath", Type: "TEXT", PK: 1, Default: json.RawMessage(`null`)},
			{CID: 1, Name: "content_sha256", Type: "TEXT", NotNull: 1, Default: json.RawMessage(`null`)},
			{CID: 2, Name: "observed_at", Type: "TEXT", NotNull: 1, Default: json.RawMessage(`"''"`)},
		}}
		f.manifest.Tables["sidecar_heads"] = table
		entry := f.manifest.Schema[0]
		entry.Type, entry.Name, entry.Table = "table", "sidecar_heads", "sidecar_heads"
		entry.SQL = "CREATE TABLE sidecar_heads(relpath TEXT PRIMARY KEY,content_sha256 TEXT NOT NULL,observed_at TEXT NOT NULL DEFAULT '')"
		f.manifest.Schema = append(f.manifest.Schema, entry)
		for _, row := range rows {
			if row["table"] == "sidecar_sources" {
				v := row["values"].(map[string]any)
				rows = append(rows, map[string]any{"table": "sidecar_heads", "key": []any{v["relpath"]}, "values": map[string]any{
					"relpath": v["relpath"], "content_sha256": v["content_sha256"], "observed_at": "2026-09-30T01:02:03.123456789-07:00"}})
			}
		}
	}
	if alter != nil {
		alter(rows)
	}
	return receiveCatalogFixtureRows(t, f, rows)
}

func advanceDocuments(t *testing.T, f *catalogSnapshotFixture, after int64) *models.CatalogDocumentImport {
	t.Helper()
	var ret *models.CatalogDocumentImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = f.repo.CatalogDocumentImport.Advance(ctx, f.manifest.UUID, f.sha, after, catalogImportNow.Add(6*time.Hour))
		return err
	}))
	return ret
}

func finishDocuments(t *testing.T, f *catalogSnapshotFixture) *models.CatalogDocumentImport {
	t.Helper()
	progress := advanceDocuments(t, f, 0)
	for progress.State == "running" {
		progress = advanceDocuments(t, f, progress.ProcessedRecords)
	}
	return progress
}

func TestCatalogDocumentImportResumesSharesAndPreservesReviewedHeads(t *testing.T) {
	f := documentImportFixture(t, 25, true, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "files", "galleries", "performers", "performers_scenes", "performers_images", "account_performer_decisions"} {
		before[table] = albumJobRows(t, raw, table)
	}
	initialRevision := queryUint(t, raw, "SELECT sum(revision) FROM source_posts")
	first := advanceDocuments(t, f, 0)
	require.Equal(t, "running", first.State)
	require.Equal(t, "sidecar_heads", first.Phase)
	require.EqualValues(t, 50, first.ProcessedRecords)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogDocumentImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		return err
	})
	require.ErrorIs(t, err, models.ErrCatalogSnapshotConflict)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	result := advanceDocuments(t, f, first.ProcessedRecords)
	require.Equal(t, "mapped", result.State)
	require.EqualValues(t, 55, result.ProcessedRecords)
	require.False(t, result.Imported)
	require.Equal(t, result, advanceDocuments(t, f, result.ProcessedRecords))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_documents"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_document_contents"))
	for _, table := range []string{"source_document_sources", "source_document_head_claims", "source_document_heads", "source_document_head_decisions"} {
		require.EqualValues(t, 27, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	require.Equal(t, initialRevision+75, queryUint(t, raw, "SELECT sum(revision) FROM source_posts"), "one source, claim and initial selection per attributed path")
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogDocumentImport.Records(ctx, f.manifest.UUID, 0, 1)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		rest, err := f.repo.CatalogDocumentImport.Records(ctx, f.manifest.UUID, rows[0].Ordinal, 100)
		require.NoError(t, err)
		require.Len(t, rest, 54)
		for _, row := range append(rows, rest...) {
			detail, err := f.repo.CatalogDocumentImport.Record(ctx, f.manifest.UUID, row.Ordinal)
			require.NoError(t, err)
			require.Equal(t, row, detail.CatalogDocumentRecord)
			require.True(t, json.Valid(detail.SourceValues))
		}
		doc, err := f.repo.SourceDocument.Find(ctx, *rows[0].DocumentUUID)
		require.NoError(t, err)
		require.Equal(t, "invalid", doc.ParseStatus)
		content, err := f.repo.SourceDocument.Content(ctx, doc.ContentSHA256)
		require.NoError(t, err)
		require.Equal(t, []byte("original\x00\xffdocument"), content)
		return nil
	}))
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: result.CollectionUUID, ExpectedRevision: 1, Origin: "review",
		SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Later name", Kind: "directory", State: "retired"}})
	postRevision := queryUint(t, raw, "SELECT sum(revision) FROM source_posts")
	replayed := advanceDocuments(t, f, result.ProcessedRecords)
	require.Equal(t, "mapped", replayed.State)
	require.Equal(t, 1, replayed.CollectionRevision)
	require.Equal(t, postRevision, queryUint(t, raw, "SELECT sum(revision) FROM source_posts"), "replay cannot create evidence again")
	require.EqualValues(t, 27, queryUint(t, raw, "SELECT count(*) FROM source_document_head_decisions"))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceDocument.DecideHead(ctx, models.SourceDocumentHeadInput{CollectionUUID: result.CollectionUUID, RelativePath: "one.nfo", ExpectedRevision: 1, State: "unlinked", Origin: "review"})
		return err
	}))
	require.Equal(t, result, advanceDocuments(t, f, result.ProcessedRecords), "historical receipts retain their original decisions after a later unlink")
	require.EqualValues(t, 27, queryUint(t, raw, "SELECT count(*) FROM source_document_sources"))
	require.EqualValues(t, 27, queryUint(t, raw, "SELECT count(*) FROM source_document_head_claims"))
	require.EqualValues(t, 28, queryUint(t, raw, "SELECT count(*) FROM source_document_head_decisions"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestCatalogDocumentImportFallbackAndAtomicFailure(t *testing.T) {
	f := documentImportFixture(t, 1, false, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := queryUint(t, raw, "SELECT sum(revision) FROM source_posts")
	attachmentSQL(t, f.db, `CREATE TRIGGER reject_document_receipt BEFORE INSERT ON catalog_document_records BEGIN SELECT RAISE(ABORT,'receipt failure'); END`)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogDocumentImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		require.ErrorContains(t, err, "receipt failure")
		return nil
	})
	require.ErrorContains(t, err, "did not finish atomically")
	for _, table := range []string{"catalog_document_imports", "catalog_document_records", "source_documents", "source_document_contents", "source_document_sources", "source_document_head_claims", "source_document_head_decisions"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	require.Equal(t, before, queryUint(t, raw, "SELECT sum(revision) FROM source_posts"))
	attachmentSQL(t, f.db, "DROP TRIGGER reject_document_receipt")
	result := finishDocuments(t, f)
	require.Equal(t, "mapped", result.State)
	require.EqualValues(t, 4, result.ProcessedRecords)
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM catalog_document_records WHERE selection_basis='legacy_fallback'"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	check := openRawDB(t, output)
	defer check.Close()
	for _, table := range []string{"catalog_document_imports", "catalog_document_records", "source_documents", "source_document_contents", "source_document_sources", "source_document_head_claims", "source_document_head_decisions"} {
		require.Zero(t, queryUint(t, check, "SELECT count(*) FROM "+table), table)
	}
}

func TestCatalogDocumentImportRetainsInvalidValuesForReview(t *testing.T) {
	f := documentImportFixture(t, 0, true, func(rows []map[string]any) {
		for _, row := range rows {
			if row["table"] == "sidecar_sources" && row["values"].(map[string]any)["relpath"] == "one.nfo" {
				row["values"].(map[string]any)["captured_at"] = "unknown historical time"
			}
		}
	})
	result := finishDocuments(t, f)
	require.Equal(t, "review", result.State)
	require.EqualValues(t, 2, result.ReviewRecords)
	require.EqualValues(t, 3, result.MappedRecords)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestCatalogDocumentImportPreservesExplicitUnlink(t *testing.T) {
	f := documentImportFixture(t, 0, true, nil)
	var collection string
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		snapshot, err := f.repo.CatalogSnapshot.Find(ctx, f.manifest.UUID)
		require.NoError(t, err)
		collection = snapshot.CollectionUUID
		_, err = f.repo.SourceDocument.DecideHead(ctx, models.SourceDocumentHeadInput{CollectionUUID: collection, RelativePath: "one.nfo", State: "unlinked", Origin: "review"})
		return err
	}))
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: collection, ExpectedRevision: 1, Origin: "review",
		SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Renamed before import", Kind: "directory", State: "retired"}})
	result := finishDocuments(t, f)
	require.Equal(t, "review", result.State)
	require.Equal(t, 1, result.CollectionRevision)
	require.EqualValues(t, 1, result.ReviewRecords)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		head, err := f.repo.SourceDocument.Head(ctx, collection, "one.nfo")
		require.NoError(t, err)
		require.Equal(t, "unlinked", head.State)
		require.Equal(t, 1, head.Revision)
		rows, err := f.repo.CatalogDocumentImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		var reviewed int
		for _, row := range rows {
			if row.Outcome == "review" {
				reviewed++
				require.Equal(t, "native_document_selection_preserved", row.Reason)
				require.NotNil(t, row.ClaimUUID)
				require.Equal(t, head.UUID, *row.HeadUUID)
			}
		}
		require.Equal(t, 1, reviewed)
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestCatalogDocumentImportForgottenPostsAndWriteGuards(t *testing.T) {
	f := documentImportFixture(t, 1, false, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("UPDATE source_posts SET state='forgotten'")
	require.NoError(t, err)
	err = f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogDocumentImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		return err
	})
	require.Error(t, err)
	for _, checkpoint := range []struct {
		sha   string
		after int64
	}{{scrape.CatalogSnapshotSHA([]byte("other manifest")), 0}, {f.sha, 1}} {
		err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.CatalogDocumentImport.Advance(ctx, f.manifest.UUID, checkpoint.sha, checkpoint.after, catalogImportNow)
			return err
		})
		require.ErrorIs(t, err, models.ErrCatalogSnapshotConflict)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_document_imports"))
	result := finishDocuments(t, f)
	require.Equal(t, "review", result.State)
	require.EqualValues(t, 1, result.ReviewRecords)
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_document_sources WHERE post_uuid IS NULL"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_document_sources WHERE post_uuid IS NOT NULL"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM catalog_document_records WHERE reason='post_forgotten'"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestCatalogDocumentImportFallbackOrderingAndExplicitPriority(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			f := snapshotFixture(t)
			rows := documentImportRows(t, f)
			var doc, source map[string]any
			for _, row := range rows {
				if row["table"] == "sidecar_documents" {
					doc = row["values"].(map[string]any)
				}
				if row["table"] == "sidecar_sources" && row["values"].(map[string]any)["relpath"] == "one.nfo" {
					source = row["values"].(map[string]any)
				}
			}
			// The legacy reader orders timestamp text, not instants in UTC.
			source["captured_at"] = "2026-09-30T00:01:00+14:00"
			newDoc, newSource := map[string]any{}, map[string]any{}
			for key, value := range doc {
				newDoc[key] = value
			}
			for key, value := range source {
				newSource[key] = value
			}
			content := []byte("newer in UTC but earlier text")
			newDoc["document_id"], newDoc["content_sha256"] = json.Number("2"), scrape.CatalogSnapshotSHA(content)
			newDoc["raw_content"] = map[string]any{"sqlite_blob_base64": base64.StdEncoding.EncodeToString(content)}
			newSource["document_id"], newSource["content_sha256"] = newDoc["document_id"], newDoc["content_sha256"]
			newSource["captured_at"] = "2026-09-29T23:59:00-12:00"
			rows = append(rows, map[string]any{"table": "sidecar_documents", "key": []any{newDoc["document_id"]}, "values": newDoc},
				map[string]any{"table": "sidecar_sources", "key": []any{newSource["relpath"], newSource["content_sha256"]}, "values": newSource})
			expectedSHA := doc["content_sha256"]
			if explicit {
				f.manifest.Tables["sidecar_heads"] = scrape.CatalogSnapshotTable{Key: []string{"relpath"}, Columns: []scrape.CatalogSnapshotColumn{
					{CID: 0, Name: "relpath", Type: "TEXT", PK: 1}, {CID: 1, Name: "content_sha256", Type: "TEXT", NotNull: 1}, {CID: 2, Name: "observed_at", Type: "TEXT", NotNull: 1}}}
				entry := f.manifest.Schema[0]
				entry.Type, entry.Name, entry.Table, entry.SQL = "table", "sidecar_heads", "sidecar_heads", "CREATE TABLE sidecar_heads(relpath TEXT PRIMARY KEY,content_sha256 TEXT NOT NULL,observed_at TEXT NOT NULL)"
				f.manifest.Schema = append(f.manifest.Schema, entry)
				rows = append(rows, map[string]any{"table": "sidecar_heads", "key": []any{"one.nfo"}, "values": map[string]any{
					"relpath": "one.nfo", "content_sha256": newDoc["content_sha256"], "observed_at": ""}})
				expectedSHA = newDoc["content_sha256"]
			}
			receiveCatalogFixtureRows(t, f, rows)
			result := finishDocuments(t, f)
			require.Equal(t, "mapped", result.State)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				head, err := f.repo.SourceDocument.Head(ctx, result.CollectionUUID, "one.nfo")
				require.NoError(t, err)
				source, err := f.repo.SourceDocument.Source(ctx, *head.SourceUUID)
				require.NoError(t, err)
				doc, err := f.repo.SourceDocument.Find(ctx, source.DocumentUUID)
				require.NoError(t, err)
				require.Equal(t, expectedSHA, doc.ContentSHA256)
				return nil
			}))
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
		})
	}
}

func TestCatalogDocumentStartupRejectsAlteredReceiptWithoutWrites(t *testing.T) {
	f := documentImportFixture(t, 0, true, nil)
	finishDocuments(t, f)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='catalog_document_record_immutable'").Scan(&guard))
	_, err := raw.Exec(`DROP TRIGGER catalog_document_record_immutable;
 UPDATE catalog_document_records SET selection_basis='legacy_fallback' WHERE selection_basis='explicit'`)
	require.NoError(t, err)
	_, err = raw.Exec(guard)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, f.db.Open(f.db.DatabasePath()), "invalid catalog document")
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}
