package sqlite_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func translationPointer(value string) *string { return &value }

func retainTranslation(t *testing.T, repo models.Repository, input models.SourceTranslationInput) *models.SourceTranslation {
	t.Helper()
	var ret *models.SourceTranslation
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceTranslation.Retain(ctx, input)
		return err
	}))
	return ret
}

func translationEvidence(repo models.Repository, input models.SourceTranslationEvidence) (*models.SourceTranslationEvidence, error) {
	var ret *models.SourceTranslationEvidence
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceTranslation.RecordEvidence(ctx, input)
		return err
	})
	return ret, err
}

func TestSourceTranslationsShareResultsWithoutInferringLanguagesOrApplyingMetadata(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "galleries", "performers", "metadata_field_decisions"} {
		before[table] = albumJobRows(t, raw, table)
	}
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "translation"}, "")
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "translation"}, "")
	input := models.SourceTranslationInput{OriginalText: translationPointer("  原文\x00\n\u2028\\u2028<&>  "), TranslatedText: "  Retained translation\n🌿  "}
	result := retainTranslation(t, repo, input)
	require.Equal(t, result, retainTranslation(t, repo, input))
	require.Equal(t, input, result.SourceTranslationInput)
	require.Equal(t, scrape.CatalogSnapshotSHA([]byte(*input.OriginalText)), *result.OriginalSHA256)
	var assertions []*models.SourceTranslationEvidence
	for _, owner := range []string{post.UUID, other.UUID} {
		evidence := models.SourceTranslationEvidence{UUID: uuid.NewString(), TranslationUUID: result.UUID, PostUUID: owner, Origin: "migration", Provenance: "legacy-original-plot-and-plot",
			CapturedAt: "2026-09-30T01:02:03.123456789-07:00", Details: json.RawMessage(`{"source":"retained"}`)}
		first, err := translationEvidence(repo, evidence)
		require.NoError(t, err)
		again, err := translationEvidence(repo, evidence)
		require.NoError(t, err)
		require.Equal(t, first, again)
		assertions = append(assertions, first)
		require.Equal(t, 2, postLinkRevision(t, repo, owner))
	}
	english := input
	english.SourceLanguage, english.TargetLanguage, english.Provider = translationPointer("ja"), translationPointer("en"), translationPointer("recorded/provider")
	known := retainTranslation(t, repo, english)
	require.NotEqual(t, result.UUID, known.UUID)
	_, err := translationEvidence(repo, models.SourceTranslationEvidence{UUID: uuid.NewString(), TranslationUUID: known.UUID, PostUUID: post.UUID, Origin: "worker", Provenance: "caption"})
	require.NoError(t, err)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := repo.SourceTranslation.PostEvidence(ctx, models.SourceTranslationQuery{PostUUID: post.UUID, OriginalSHA256: *known.OriginalSHA256, TargetLanguage: translationPointer("en"), Limit: 100})
		require.NoError(t, err)
		require.Len(t, rows, 1, "unknown target language is not English")
		require.Equal(t, known.UUID, rows[0].TranslationUUID)
		first, err := repo.SourceTranslation.PostEvidence(ctx, models.SourceTranslationQuery{PostUUID: post.UUID, Limit: 1})
		require.NoError(t, err)
		require.Len(t, first, 1)
		rest, err := repo.SourceTranslation.PostEvidence(ctx, models.SourceTranslationQuery{PostUUID: post.UUID, After: first[0].UUID, Limit: 1})
		require.NoError(t, err)
		require.Len(t, rest, 1)
		require.NotEqual(t, first[0].UUID, rest[0].UUID)
		body, err := json.Marshal(append(first, rest...))
		require.NoError(t, err)
		require.NotContains(t, string(body), "translated_text")
		return nil
	}))
	for _, optional := range []*string{nil, translationPointer("")} {
		retained := retainTranslation(t, repo, models.SourceTranslationInput{OriginalText: optional, TranslatedText: ""})
		require.Equal(t, optional, retained.OriginalText)
		require.Equal(t, optional == nil, retained.OriginalSHA256 == nil)
	}
	require.EqualValues(t, 4, queryUint(t, raw, "SELECT count(*) FROM source_translations"))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	backup := filepath.Join(t.TempDir(), "translations-backup.sqlite")
	require.NoError(t, db.Backup(backup))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(backup))
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, expected := range []*models.SourceTranslation{result, known} {
			actual, err := repo.SourceTranslation.Find(ctx, expected.UUID)
			require.NoError(t, err)
			require.Equal(t, expected, actual, "normal backups retain exact text and nullable language/provider facts")
		}
		for _, expected := range assertions {
			actual, err := repo.SourceTranslation.Evidence(ctx, expected.UUID)
			require.NoError(t, err)
			require.Equal(t, expected, actual)
		}
		return nil
	}))
}

func TestSourceTranslationReplayForgottenScopeAndAtomicFailure(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "translation"}, "")
	input := models.SourceTranslationInput{OriginalText: translationPointer("original"), TranslatedText: "translated"}
	result := retainTranslation(t, repo, input)
	evidence := models.SourceTranslationEvidence{UUID: uuid.NewString(), TranslationUUID: result.UUID, PostUUID: post.UUID, Origin: "migration", Provenance: "legacy"}
	first, err := translationEvidence(repo, evidence)
	require.NoError(t, err)
	changed := evidence
	changed.Provenance = "changed"
	_, err = translationEvidence(repo, changed)
	require.ErrorIs(t, err, models.ErrSourceTranslationReplay)
	_, err = repo.SourceTranslation.Retain(t.Context(), input)
	require.Error(t, err, "writes require a managed transaction")
	for _, invalid := range []models.SourceTranslationInput{{OriginalText: translationPointer(string([]byte{0xff})), TranslatedText: "text"}, {TranslatedText: strings.Repeat("a", archive.MaxTranslationTextBytes+1)}} {
		_, err := archive.PrepareSourceTranslation(invalid)
		require.ErrorIs(t, err, models.ErrSourceTranslationInvalid)
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE source_translations SET translated_text='changed'")
	require.Error(t, err)
	attachmentSQL(t, db, "CREATE TRIGGER reject_translation_evidence BEFORE INSERT ON source_translation_evidence BEGIN SELECT RAISE(ABORT,'fixture evidence failure'); END")
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		another, err := repo.SourceTranslation.Retain(ctx, models.SourceTranslationInput{TranslatedText: "Distinct failed result"})
		require.NoError(t, err)
		_, err = repo.SourceTranslation.RecordEvidence(ctx, models.SourceTranslationEvidence{UUID: uuid.NewString(), TranslationUUID: another.UUID, PostUUID: post.UUID, Origin: "migration"})
		require.ErrorContains(t, err, "fixture evidence failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrSourceTranslationAtomic)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_translations"))
	attachmentSQL(t, db, "DROP TRIGGER reject_translation_evidence")
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", post.UUID)
	require.NoError(t, err)
	again, err := translationEvidence(repo, evidence)
	require.NoError(t, err)
	require.Equal(t, first, again)
	evidence.UUID = uuid.NewString()
	_, err = translationEvidence(repo, evidence)
	require.ErrorIs(t, err, models.ErrSourcePostForgotten)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_translation_evidence"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestSourceTranslationStartupRefusesAlteredResultWithoutWrites(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	retainTranslation(t, repo, models.SourceTranslationInput{TranslatedText: "Retained text"})
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='source_translation_immutable'").Scan(&guard))
	_, err := raw.Exec("DROP TRIGGER source_translation_immutable; UPDATE source_translations SET translated_text='Changed text';" + guard)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	check := sqlite.NewDatabase()
	require.ErrorIs(t, check.Open(path), models.ErrSourcePayloadCorrupt)
	require.NoError(t, check.Close())
	after, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	require.Equal(t, before, after)
}
