package sqlite_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeImportedNFOSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP TRIGGER catalog_nfo_compaction_guard;
DROP VIEW catalog_document_pending_imports;
ALTER TABLE catalog_snapshots DROP COLUMN nfo_compacted;
DELETE FROM native_migration_history WHERE version=1000100`)
	require.NoError(t, err)
}

func cleanupFixture(t *testing.T) (*sqlite.Database, models.Repository, string, string) {
	t.Helper()
	db, repo, collection, post, docInput := documentFixture(t)
	docInput.Parsed = json.RawMessage(`{"title":["Original title"],"original-plot":["Original text"],"plot":["Translated text"],"url":["https://example.com/post"]}`)
	doc := retainDocument(t, repo, docInput)
	recordDocumentSource(t, repo, models.SourceDocumentSource{UUID: uuid.NewString(), DocumentUUID: doc.UUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, PostUUID: &post.UUID, RelativePath: "one.nfo", Origin: "migration"})
	data, err := archive.CollateLegacyNFOFields(docInput.Parsed)
	require.NoError(t, err)
	payload, err := archive.PrepareRetainedCapture("legacy-nfo", "reddit", json.RawMessage(`{"nfo_fields":`+string(docInput.Parsed)+`,"nfo_path":"one.nfo"}`))
	require.NoError(t, err)
	capture := recordSourceTestCapture(t, repo, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "legacy-nfo", Platform: "reddit", CapturedAt: time.Now().UTC(), RetentionPolicy: "legacy-retained-v1", Metadata: data.Metadata, Payload: *payload})
	return db, repo, post.UUID, capture.UUID
}

func TestImportedNFOCleanupPreservesMetadataAndTranslations(t *testing.T) {
	db, repo, post, captureID := cleanupFixture(t)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	before := albumJobRows(t, raw, "scenes")
	result, err := db.CompactImportedNFO(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Captures)
	require.Equal(t, 1, result.Documents)
	require.Equal(t, before, albumJobRows(t, raw, "scenes"))
	for _, table := range []string{"source_documents", "source_document_sources", "source_document_contents", "catalog_document_records"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		capture, err := repo.SourceEvidence.FindCapture(ctx, captureID)
		require.NoError(t, err)
		require.Equal(t, post, capture.PostUUID)
		require.Equal(t, "Original text", *capture.Metadata.OriginalText)
		require.JSONEq(t, `{}`, string(capture.Payload.Patch))
		evidence, err := repo.SourceTranslation.PostEvidence(ctx, models.SourceTranslationQuery{PostUUID: post})
		require.NoError(t, err)
		require.Len(t, evidence, 1)
		translation, err := repo.SourceTranslation.Find(ctx, evidence[0].TranslationUUID)
		require.NoError(t, err)
		require.Equal(t, "Translated text", translation.TranslatedText)
		require.Nil(t, translation.TargetLanguage)
		require.JSONEq(t, `{"display_translations":{"original_text":"`+translation.UUID+`"}}`, string(capture.Payload.Shared))
		return nil
	}))
	second, err := db.CompactImportedNFO(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, &sqlite.NFOCleanupResult{}, second)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestImportedNFOCleanupRollsBackOnUnaccountedDocument(t *testing.T) {
	db, repo, _, captureID := cleanupFixture(t)
	retainDocument(t, repo, models.SourceDocumentInput{Content: []byte("unmapped"), Encoding: "utf-8", Warnings: json.RawMessage(`[]`), Parser: "legacy-catalog-nfo-v1", ParseStatus: "valid", Parsed: json.RawMessage(`{"unexpected":["keep me"]}`)})
	_, err := db.CompactImportedNFO(t.Context(), nil)
	require.ErrorContains(t, err, "unmapped NFO field")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		capture, err := repo.SourceEvidence.FindCapture(ctx, captureID)
		require.NoError(t, err)
		require.Contains(t, string(capture.Payload.Shared), "nfo_fields")
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_documents"))
	_, err = raw.Exec("UPDATE source_captures SET origin='changed'")
	require.ErrorContains(t, err, "immutable")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestImportedNFOCleanupPreservesAbsentEmptyScalars(t *testing.T) {
	db, repo, post, _ := cleanupFixture(t)
	payload, err := archive.PrepareRetainedCapture("legacy-nfo", "reddit", json.RawMessage(`{"nfo_fields":{"title":[""],"plot":[""],"premiered":[""]},"nfo_path":"empty.nfo"}`))
	require.NoError(t, err)
	capture := recordSourceTestCapture(t, repo, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post, Origin: "legacy-nfo", Platform: "reddit", CapturedAt: time.Now().UTC(), RetentionPolicy: "legacy-retained-v1", Payload: *payload})
	_, err = db.CompactImportedNFO(t.Context(), nil)
	require.NoError(t, err)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		kept, err := repo.SourceEvidence.FindCapture(ctx, capture.UUID)
		require.NoError(t, err)
		require.Equal(t, models.SourcePostMetadata{}, kept.Metadata)
		return nil
	}))
}

func TestImportedNFOCleanupCompletedSnapshotCanReopen(t *testing.T) {
	empty := sha256.Sum256(nil)
	hash := hex.EncodeToString(empty[:])
	f := documentImportFixture(t, 0, true, func(rows []map[string]any) {
		for _, row := range rows {
			if row["table"] == "observations" {
				v := row["values"].(map[string]any)
				if v["origin"] == "legacy-nfo" {
					v["title"] = "Original title"
					v["published_at"] = nil
					v["date_basis"] = nil
				}
			}
			if !strings.HasPrefix(row["table"].(string), "sidecar") {
				continue
			}
			v := row["values"].(map[string]any)
			v["content_sha256"] = hash
			switch row["table"] {
			case "sidecar_documents":
				v["raw_content"] = map[string]any{"sqlite_blob_base64": base64.StdEncoding.EncodeToString(nil)}
				v["parse_status"] = "empty"
				v["warnings_json"] = "[]"
			case "sidecar_sources":
				row["key"] = []any{v["relpath"], hash}
			}
		}
	})
	finishDocuments(t, f)
	result, err := f.db.CompactImportedNFO(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Catalogs)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_snapshot_records WHERE source_table LIKE 'sidecar%'"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM catalog_snapshots WHERE nfo_compacted=1"))
	_, err = raw.Exec("UPDATE catalog_snapshots SET nfo_compacted=0")
	require.Error(t, err)
}

func TestImportedNFOCleanupRecoversMalformedPostAndExistingMedia(t *testing.T) {
	f := newSourceFileFixture(t)
	recordContentClaim(t, f.repo, f.claim)
	observation := recordFileObservation(t, f.repo, f.observation)
	file := archiveFind(t, f.repo, models.ArchiveFile, 21)
	recordFileMatch(t, f.repo, models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: observation.UUID, FileUUID: file.UUID, Generation: 1, LibraryRootPath: "/identity-fixture", Basis: "exact-path", Origin: "migration"})
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := f.db.ExecSQL(ctx, `INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(31,21,1)`, nil)
		return err
	}))
	doc := retainDocument(t, f.repo, models.SourceDocumentInput{Content: []byte(`<movie><url>https://coomer.st/onlyfans/user/account/post/post-id</url><premiered>2025-01-02</premiered><title>Original & title</title><plot>Original <3 text</plot></movie>`), Encoding: "utf-8", Parser: "legacy-catalog-nfo-v1", ParseStatus: "invalid", Warnings: json.RawMessage(`[]`), Parsed: json.RawMessage(`{}`)})
	for _, path := range []string{"video.nfo", "missing.nfo"} {
		recordDocumentSource(t, f.repo, models.SourceDocumentSource{UUID: uuid.NewString(), DocumentUUID: doc.UUID, CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RelativePath: path, Origin: "migration"})
	}
	result, err := f.db.CompactImportedNFO(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.RecoveredDocuments)
	require.Equal(t, 1, result.RecoveredMedia)
	require.Equal(t, 1, result.MissingMedia)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		post, err := f.repo.SourceEvidence.FindPostByIdentifier(ctx, models.SourcePostIdentifier{Namespace: "mirror:coomer:onlyfans", Value: "account/post-id"})
		require.NoError(t, err)
		require.NotNil(t, post)
		captures, err := f.repo.SourceEvidence.Captures(ctx, post.UUID, nil, 100)
		require.NoError(t, err)
		require.Len(t, captures, 1)
		require.Equal(t, "Original <3 text", *captures[0].Metadata.OriginalText)
		require.True(t, captures[0].CapturedAt.IsZero())
		media, err := f.repo.SourcePostMedia.MediaForPost(ctx, post.UUID, "", 100)
		require.NoError(t, err)
		require.Len(t, media, 1)
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}
