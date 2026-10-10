package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func retainDocument(t *testing.T, repo models.Repository, input models.SourceDocumentInput) *models.SourceDocument {
	t.Helper()
	var ret *models.SourceDocument
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceDocument.Retain(ctx, input)
		return err
	}))
	return ret
}

func documentFixture(t *testing.T) (*sqlite.Database, models.Repository, *models.SourceCollection, *models.SourcePost, models.SourceDocumentInput) {
	t.Helper()
	db, repo := archiveTestDatabase(t)
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Retained documents", Kind: "directory", State: "disabled"}})
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "document-post"}, "")
	input := models.SourceDocumentInput{Content: []byte(strings.Repeat("<title>Retained source text</title>\n", 100)), Encoding: "utf-8", Parser: "legacy-catalog-nfo-v1", ParseStatus: "valid",
		Warnings: json.RawMessage(`[]`), Parsed: json.RawMessage(`{"title":["Retained source text"]}`)}
	return db, repo, collection, post, input
}

func recordDocumentSource(t *testing.T, repo models.Repository, input models.SourceDocumentSource) *models.SourceDocumentSource {
	t.Helper()
	var ret *models.SourceDocumentSource
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceDocument.RecordSource(ctx, input)
		return err
	}))
	return ret
}

func TestSourceDocumentsRetainSharedBytesPathsAndExactInterpretations(t *testing.T) {
	db, repo, collection, post, input := documentFixture(t)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	before := map[string]uint{}
	for _, table := range []string{"files", "images", "scenes"} {
		before[table] = queryUint(t, raw, "SELECT count(*) FROM "+table)
	}
	doc := retainDocument(t, repo, input)
	require.Equal(t, doc, retainDocument(t, repo, input))
	input.ParseStatus, input.Warnings = "repaired", json.RawMessage(`["Retained parser warning"]`)
	repaired := retainDocument(t, repo, input)
	require.NotEqual(t, doc.UUID, repaired.UUID)
	require.Equal(t, doc.ContentSHA256, repaired.ContentSHA256)
	empty := models.SourceDocumentInput{Parser: "fixture", ParseStatus: "empty", Warnings: json.RawMessage(`[]`), Parsed: json.RawMessage(`{}`)}
	emptyDoc := retainDocument(t, repo, empty)
	require.Equal(t, emptyDoc, retainDocument(t, repo, empty))
	source := models.SourceDocumentSource{UUID: uuid.NewString(), DocumentUUID: doc.UUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
		RelativePath: "collection\\literal/one.nfo", PostUUID: &post.UUID, CapturedAt: "2026-09-29T02:03:04.123456789-07:00", Origin: "migration", Details: json.RawMessage(`{"original":true}`)}
	first := recordDocumentSource(t, repo, source)
	require.Equal(t, first, recordDocumentSource(t, repo, source))
	source.UUID, source.RelativePath, source.PostUUID = uuid.NewString(), "unattributed/folder.nfo", nil
	second := recordDocumentSource(t, repo, source)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		found, err := repo.SourceDocument.Find(ctx, repaired.UUID)
		require.NoError(t, err)
		require.Equal(t, repaired, found)
		body, err := repo.SourceDocument.Content(ctx, doc.ContentSHA256)
		require.NoError(t, err)
		require.Equal(t, input.Content, body)
		body, err = repo.SourceDocument.Content(ctx, emptyDoc.ContentSHA256)
		require.NoError(t, err)
		require.NotNil(t, body)
		require.Empty(t, body)
		paths, err := repo.SourceDocument.PostSources(ctx, post.UUID, "", 1)
		require.NoError(t, err)
		require.Equal(t, []models.SourceDocumentSource{*first}, paths)
		paths, err = repo.SourceDocument.PostSources(ctx, post.UUID, first.UUID, 1)
		require.NoError(t, err)
		require.Empty(t, paths)
		paths, err = repo.SourceDocument.LocationSources(ctx, collection.UUID, source.RelativePath, "", 1)
		require.NoError(t, err)
		require.Equal(t, []models.SourceDocumentSource{*second}, paths)
		_, err = repo.SourceDocument.LocationSources(ctx, collection.UUID, source.RelativePath, "invalid", 1)
		require.Error(t, err)
		_, err = repo.SourceDocument.PostSources(ctx, post.UUID, "", 101)
		require.Error(t, err)
		return nil
	}))
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM source_document_contents"))
	require.Equal(t, uint(3), queryUint(t, raw, "SELECT count(*) FROM source_documents"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_document_contents WHERE storage_encoding='gzip'"))
	for table, count := range before {
		require.Equal(t, count, queryUint(t, raw, "SELECT count(*) FROM "+table), "document evidence must not create media: %s", table)
	}
	for _, item := range []struct {
		query, index string
		args         []any
	}{
		{"SELECT uuid FROM source_document_sources WHERE post_uuid=? AND uuid>? ORDER BY uuid LIMIT 10", "source_document_sources_post", []any{post.UUID, ""}},
		{"SELECT uuid FROM source_document_sources WHERE collection_uuid=? AND relative_path=? AND uuid>? ORDER BY uuid LIMIT 10", "source_document_sources_location", []any{collection.UUID, source.RelativePath, ""}},
		{"SELECT uuid FROM source_document_head_claims WHERE collection_uuid=? AND relative_path=? AND uuid>? ORDER BY uuid LIMIT 10", "source_document_head_claims_location", []any{collection.UUID, source.RelativePath, ""}},
	} {
		var id, parent, unused int
		var plan string
		require.NoError(t, raw.QueryRow("EXPLAIN QUERY PLAN "+item.query, item.args...).Scan(&id, &parent, &unused, &plan))
		require.Contains(t, plan, item.index)
		require.NotContains(t, plan, "SCAN ")
	}
}

func TestSourceDocumentMigrationPreservesLibraryAndRollsBackOnCollision(t *testing.T) {
	for _, collision := range []bool{false, true} {
		name := "upgrade"
		if collision {
			name = "collision"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schema40.sqlite")
			buildLegacyDatabase(t, path, 86, true)
			db := sqlite.NewDatabase()
			defer db.Close()
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(path), &needed))
			m, err := sqlite.NewMigrator(db)
			require.NoError(t, err)
			for v := m.CurrentSchemaVersion(); v < sqlite.NativeSchemaBaseline+4; v = m.CurrentSchemaVersion() {
				require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(v)))
			}
			raw := openRawDB(t, path)
			defer raw.Close()
			_, err = raw.Exec(archiveIdentityFixture)
			require.NoError(t, err)
			// Insert media before the metadata migrations so those migrations
			// establish the same complete field decisions as a real old library.
			for v := m.CurrentSchemaVersion(); v < sqlite.NativeSchemaBaseline+40; v = m.CurrentSchemaVersion() {
				require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(v)))
			}
			m.Close()
			before := map[string][][]any{}
			for _, table := range []string{"performers", "performer_names", "scenes", "images", "files", "archive_entities"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE source_document_sources(retained TEXT); INSERT INTO source_document_sources VALUES('Original unrelated data')")
				require.NoError(t, err)
				require.Error(t, db.RunAllMigrations())
				require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty=1"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name IN ('source_document_contents','source_documents')"), "failed DDL leaves no partial new tables")
				var retained string
				require.NoError(t, raw.QueryRow("SELECT retained FROM source_document_sources").Scan(&retained))
				require.Equal(t, "Original unrelated data", retained)
				check := sqlite.NewDatabase()
				require.Error(t, check.Open(path), "normal startup refuses a dirty upgrade")
				require.NoError(t, check.Close())
			} else {
				require.NoError(t, db.RunAllMigrations())
				require.NoError(t, db.ReInitialise())
				require.Equal(t, sqlite.GetRequiredSchemaVersion(), db.Version())
				require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000041"))
				for _, table := range []string{"source_document_contents", "source_documents", "source_document_sources", "source_document_head_claims", "source_document_head_decisions", "source_document_heads"} {
					require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
				}
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}

func TestSourceDocumentHeadsKeepClaimsAndProtectExplicitChoices(t *testing.T) {
	db, repo, collection, post, input := documentFixture(t)
	doc := retainDocument(t, repo, input)
	source := recordDocumentSource(t, repo, models.SourceDocumentSource{UUID: uuid.NewString(), DocumentUUID: doc.UUID, CollectionUUID: collection.UUID,
		CollectionRevision: collection.Revision, RelativePath: "folder/item.nfo", PostUUID: &post.UUID, Origin: "migration"})
	var claim *models.SourceDocumentHeadClaim
	claimInput := models.SourceDocumentHeadClaim{UUID: uuid.NewString(), SourceUUID: source.UUID, CollectionUUID: collection.UUID, RelativePath: source.RelativePath,
		ObservedAt: "2026-09-29T01:02:03-07:00", Origin: "migration"}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		claim, err = repo.SourceDocument.RecordHeadClaim(ctx, claimInput)
		return err
	}))
	choice := models.SourceDocumentHeadInput{CollectionUUID: collection.UUID, RelativePath: source.RelativePath, State: "linked", SourceUUID: source.UUID, ClaimUUID: claim.UUID, Origin: "migration"}
	var linked, unlinked *models.SourceDocumentHead
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		linked, err = repo.SourceDocument.DecideHead(ctx, choice)
		return err
	}))
	choice.ExpectedRevision, choice.State, choice.SourceUUID, choice.ClaimUUID, choice.Origin = linked.Revision, "unlinked", "", "", "review"
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		unlinked, err = repo.SourceDocument.DecideHead(ctx, choice)
		return err
	}))
	for _, origin := range []string{"capture", "migration"} {
		choice.ExpectedRevision, choice.State, choice.SourceUUID, choice.Origin = unlinked.Revision, "linked", source.UUID, origin
		err := repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := repo.SourceDocument.DecideHead(ctx, choice); return err })
		require.ErrorIs(t, err, models.ErrSourceDocumentConflict)
	}
	claimInput.UUID, claimInput.ObservedAt = uuid.NewString(), "2026-10-01T01:02:03Z"
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceDocument.RecordHeadClaim(ctx, claimInput)
		return err
	}))
	backup := filepath.Join(t.TempDir(), "document-backup.sqlite")
	require.NoError(t, db.Backup(backup))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(backup))
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		body, err := repo.SourceDocument.Content(ctx, doc.ContentSHA256)
		require.NoError(t, err)
		require.Equal(t, input.Content, body, "a normal database backup includes exact document bytes")
		current, err := repo.SourceDocument.Head(ctx, collection.UUID, source.RelativePath)
		require.NoError(t, err)
		require.Equal(t, unlinked, current)
		history, err := repo.SourceDocument.HeadHistory(ctx, collection.UUID, source.RelativePath, 0, 100)
		require.NoError(t, err)
		require.Equal(t, []models.SourceDocumentHead{*linked, *unlinked}, history)
		history, err = repo.SourceDocument.HeadHistory(ctx, collection.UUID, source.RelativePath, 1, 1)
		require.NoError(t, err)
		require.Equal(t, []models.SourceDocumentHead{*unlinked}, history)
		claims, err := repo.SourceDocument.HeadClaims(ctx, collection.UUID, source.RelativePath, "", 100)
		require.NoError(t, err)
		require.Len(t, claims, 2, "new source assertions survive without changing the native choice")
		return nil
	}))
	choice.Origin, choice.ExpectedRevision = "review", linked.Revision
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := repo.SourceDocument.DecideHead(ctx, choice); return err })
	require.ErrorIs(t, err, models.ErrSourceDocumentConflict)
}

func TestSourceDocumentScopeReplayForgottenAndAtomicFailure(t *testing.T) {
	db, repo, collection, post, input := documentFixture(t)
	doc := retainDocument(t, repo, input)
	sourceInput := models.SourceDocumentSource{UUID: uuid.NewString(), DocumentUUID: doc.UUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
		RelativePath: "item.nfo", PostUUID: &post.UUID, Origin: "migration"}
	source := recordDocumentSource(t, repo, sourceInput)
	sourceInput.RelativePath = "changed.nfo"
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceDocument.RecordSource(ctx, sourceInput)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceDocumentReplay)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE source_document_sources SET relative_path='changed.nfo' WHERE uuid=?", source.UUID)
	require.Error(t, err)
	claim := models.SourceDocumentHeadClaim{UUID: uuid.NewString(), SourceUUID: source.UUID, CollectionUUID: collection.UUID, RelativePath: "wrong.nfo", Origin: "migration"}
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := repo.SourceDocument.RecordHeadClaim(ctx, claim); return err })
	require.ErrorIs(t, err, models.ErrSourceDocumentInvalid)
	_, err = raw.Exec(`INSERT INTO source_document_head_claims(uuid,source_uuid,collection_uuid,relative_path,observed_at,origin,details) VALUES(?,?,?,'wrong.nfo','','migration','{}')`, uuid.NewString(), source.UUID, collection.UUID)
	require.Error(t, err, "the database independently enforces location scope")
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", post.UUID)
	require.NoError(t, err)
	require.Equal(t, source, recordDocumentSource(t, repo, *source), "exact evidence replay does not revive a forgotten post")
	sourceInput.UUID = uuid.NewString()
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceDocument.RecordSource(ctx, sourceInput)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourcePostForgotten)
	claim.RelativePath = source.RelativePath
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := repo.SourceDocument.RecordHeadClaim(ctx, claim); return err })
	require.ErrorIs(t, err, models.ErrSourcePostForgotten)
	_, err = raw.Exec(`CREATE TRIGGER reject_document_fixture BEFORE INSERT ON source_documents BEGIN SELECT RAISE(ABORT,'fixture failure'); END`)
	require.NoError(t, err)
	input.Content = []byte("distinct content for failed transaction")
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceDocument.Retain(ctx, input)
		require.Error(t, err)
		return nil // The repository must also protect callers that ignore this error.
	})
	require.ErrorIs(t, err, models.ErrSourceDocumentConflict)
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_document_contents"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_document_head_claims"))
	_, err = raw.Exec("DROP TRIGGER reject_document_fixture")
	require.NoError(t, err)
	err = repo.WithReadTxn(t.Context(), func(ctx context.Context) error { _, err := repo.SourceDocument.Retain(ctx, input); return err })
	require.Error(t, err)
}

func TestSourceDocumentValidationRejectsMissingIndexAndCorruptedBytes(t *testing.T) {
	for _, corruption := range []string{"index", "content", "interpretation"} {
		t.Run(corruption, func(t *testing.T) {
			db, repo, _, _, input := documentFixture(t)
			retainDocument(t, repo, input)
			path := db.DatabasePath()
			require.NoError(t, db.Close())
			raw := openRawDB(t, path)
			switch corruption {
			case "index":
				_, err := raw.Exec("DROP INDEX source_document_sources_location")
				require.NoError(t, err)
			case "content":
				var guard string
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='source_document_content_immutable'").Scan(&guard))
				_, err := raw.Exec("DROP TRIGGER source_document_content_immutable; UPDATE source_document_contents SET data=x'010203'")
				require.NoError(t, err)
				_, err = raw.Exec(guard)
				require.NoError(t, err)
			case "interpretation":
				var guard string
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='source_document_immutable'").Scan(&guard))
				_, err := raw.Exec("DROP TRIGGER source_document_immutable; UPDATE source_documents SET parsed='{}'")
				require.NoError(t, err)
				_, err = raw.Exec(guard)
				require.NoError(t, err)
			}
			require.NoError(t, raw.Close())
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			if corruption == "index" {
				require.Error(t, db.Open(path))
			} else {
				require.Error(t, db.AuditForTesting(path))
			}
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestSourceDocumentsRemovedFromAnonymisedArchive(t *testing.T) {
	db, repo, collection, post, input := documentFixture(t)
	doc := retainDocument(t, repo, input)
	source := recordDocumentSource(t, repo, models.SourceDocumentSource{UUID: uuid.NewString(), DocumentUUID: doc.UUID, CollectionUUID: collection.UUID,
		CollectionRevision: collection.Revision, RelativePath: "private-document-path.nfo", PostUUID: &post.UUID, Origin: "migration"})
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		claim, err := repo.SourceDocument.RecordHeadClaim(ctx, models.SourceDocumentHeadClaim{UUID: uuid.NewString(), SourceUUID: source.UUID,
			CollectionUUID: collection.UUID, RelativePath: source.RelativePath, Origin: "migration"})
		if err != nil {
			return err
		}
		_, err = repo.SourceDocument.DecideHead(ctx, models.SourceDocumentHeadInput{CollectionUUID: collection.UUID, RelativePath: source.RelativePath,
			State: "linked", SourceUUID: source.UUID, ClaimUUID: claim.UUID, Origin: "migration"})
		return err
	}))
	path := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(db, path)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	raw := openRawDB(t, path)
	defer raw.Close()
	for _, table := range []string{"source_document_contents", "source_documents", "source_document_sources", "source_document_head_claims", "source_document_head_decisions", "source_document_heads"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(contents), "private-document-path.nfo")
	require.NotContains(t, string(contents), "Retained source text")
	check := sqlite.NewDatabase()
	require.NoError(t, check.Open(path))
	require.NoError(t, check.Close())
}
