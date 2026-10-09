package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func retainTranslationRequest(t *testing.T, repo models.Repository, original string) *models.TranslationRequest {
	t.Helper()
	var ret *models.TranslationRequest
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.TranslationWork.RetainRequest(ctx, models.TranslationRequestInput{OriginalText: original, TargetLanguage: "en", Policy: models.TranslationBingTextV1})
		return err
	}))
	return ret
}

func retainTranslationCache(t *testing.T, repo models.Repository, input models.TranslationCacheInput) *models.TranslationCache {
	t.Helper()
	var ret *models.TranslationCache
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.TranslationWork.RetainCache(ctx, input)
		return err
	}))
	return ret
}

func retainTranslationTarget(t *testing.T, repo models.Repository, input models.TranslationTargetInput, schedule models.TranslationTargetSchedule, now time.Time) *models.TranslationTarget {
	t.Helper()
	var ret *models.TranslationTarget
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.TranslationWork.RetainTarget(ctx, input, schedule, now)
		return err
	}))
	return ret
}

func publishTranslationTarget(repo models.Repository, target *models.TranslationTarget, now time.Time) (*models.TranslationTarget, error) {
	var ret *models.TranslationTarget
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.TranslationWork.PublishTarget(ctx, target.UUID, target.Revision, now)
		return err
	})
	return ret, err
}

func TestTranslationWorkSharesResultsPreservesHoldsAndRestoresBackup(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "galleries", "performers", "metadata_field_decisions"} {
		before[table] = albumJobRows(t, raw, table)
	}
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "translation-work"}, "")
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "translation-work"}, "")
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Original source", Kind: "directory", State: "disabled"}})
	original := "  原文\x00\n\u2028\\u2028<&>  "
	request := retainTranslationRequest(t, repo, original)
	require.Equal(t, request, retainTranslationRequest(t, repo, original))
	require.Equal(t, scrape.CatalogSnapshotSHA([]byte(original)), request.OriginalSHA256)
	now := time.Date(2026, 10, 2, 1, 2, 3, 123456789, time.UTC)
	heldSchedule := models.TranslationTargetSchedule{State: "held", Priority: 25, NotBefore: now.Add(time.Hour)}
	held := retainTranslationTarget(t, repo, models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: post.UUID, CollectionUUID: &collection.UUID, CollectionRevision: &collection.Revision, Field: "title", Origin: "migration"}, heldSchedule, now)
	ready := retainTranslationTarget(t, repo, models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: other.UUID, Field: "caption", Origin: "capture"}, models.TranslationTargetSchedule{State: "pending", Priority: 100}, now)
	cacheInput := models.TranslationCacheInput{RequestUUID: request.UUID, Status: "translated", TranslatedText: translationPointer("  Shared translation\n🌿  "), SourceLanguage: translationPointer("ja"), Provider: translationPointer("translate-shell/bing"), CapturedAt: "2026-09-30T03:04:05.123456789-07:00", Origin: "migration"}
	cache := retainTranslationCache(t, repo, cacheInput)
	cacheInput.Origin, cacheInput.CapturedAt = "worker", "2026-10-02T10:00:00Z"
	require.Equal(t, cache, retainTranslationCache(t, repo, cacheInput), "replay keeps the first provider timestamp and provenance")
	require.Equal(t, held, retainTranslationTarget(t, repo, held.TranslationTargetInput, models.TranslationTargetSchedule{State: "pending", Priority: 100}, now.Add(time.Minute)), "new captures cannot release a held target")
	_, err := publishTranslationTarget(repo, held, now.Add(2*time.Hour))
	require.ErrorIs(t, err, models.ErrTranslationWorkConflict)
	ready, err = publishTranslationTarget(repo, ready, now)
	require.NoError(t, err)
	require.Equal(t, "completed", ready.State)
	require.NotNil(t, ready.EvidenceUUID)
	require.Equal(t, cache.UUID, *ready.CacheUUID)
	require.Equal(t, 1, postLinkRevision(t, repo, post.UUID))
	require.Equal(t, 2, postLinkRevision(t, repo, other.UUID))
	putSourceCollection(t, repo, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Renamed source", Kind: "directory", State: "retired"}})
	var scheduled *models.TranslationTarget
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		schedule := heldSchedule
		schedule.State = "pending"
		var err error
		scheduled, err = repo.TranslationWork.ScheduleTarget(ctx, held.UUID, held.Revision, schedule, now.Add(time.Minute))
		return err
	}))
	require.Equal(t, held.NotBefore, scheduled.NotBefore)
	require.Equal(t, held.Priority, scheduled.Priority)
	_, err = publishTranslationTarget(repo, scheduled, now.Add(time.Minute))
	require.ErrorIs(t, err, models.ErrTranslationWorkConflict, "cached work still respects its retry deadline")
	completed, err := publishTranslationTarget(repo, scheduled, held.NotBefore)
	require.NoError(t, err)
	require.NotEqual(t, ready.EvidenceUUID, completed.EvidenceUUID)
	require.Equal(t, cache.UUID, *completed.CacheUUID)
	require.Equal(t, 2, postLinkRevision(t, repo, post.UUID))
	repeated, err := publishTranslationTarget(repo, completed, held.NotBefore.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, completed, repeated)
	require.Equal(t, 2, postLinkRevision(t, repo, post.UUID))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM translation_requests"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM translation_cache"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_translations"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_translation_evidence"))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	backup := filepath.Join(t.TempDir(), "translation-work-backup.sqlite")
	require.NoError(t, db.Backup(backup))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(backup))
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		foundRequest, err := repo.TranslationWork.Request(ctx, request.UUID)
		require.NoError(t, err)
		require.Equal(t, request, foundRequest)
		foundCache, err := repo.TranslationWork.Cache(ctx, request.UUID)
		require.NoError(t, err)
		require.Equal(t, cache, foundCache)
		foundTarget, err := repo.TranslationWork.Target(ctx, held.UUID)
		require.NoError(t, err)
		require.Equal(t, completed, foundTarget)
		evidence, err := repo.SourceTranslation.Evidence(ctx, *completed.EvidenceUUID)
		require.NoError(t, err)
		require.Equal(t, collection.Revision, *evidence.CollectionRevision, "later source edits do not rewrite historical provenance")
		require.Equal(t, cache.CapturedAt, evidence.CapturedAt)
		first, err := repo.TranslationWork.Targets(ctx, models.TranslationTargetQuery{RequestUUID: request.UUID, State: "completed", Limit: 1})
		require.NoError(t, err)
		require.Len(t, first, 1)
		rest, err := repo.TranslationWork.Targets(ctx, models.TranslationTargetQuery{RequestUUID: request.UUID, State: "completed", After: first[0].UUID, Limit: 1})
		require.NoError(t, err)
		require.Len(t, rest, 1)
		require.NotEqual(t, first[0].UUID, rest[0].UUID)
		body, err := json.Marshal(append(first, rest...))
		require.NoError(t, err)
		require.NotContains(t, string(body), "translated_text")
		require.NotContains(t, string(body), "original_text")
		byPost, err := repo.TranslationWork.Targets(ctx, models.TranslationTargetQuery{PostUUID: post.UUID})
		require.NoError(t, err)
		require.Equal(t, []models.TranslationTarget{*completed}, byPost)
		history, err := repo.TranslationWork.TargetHistory(ctx, held.UUID, 0, 2)
		require.NoError(t, err)
		require.Len(t, history, 2)
		require.Equal(t, "held", history[0].State)
		require.Equal(t, "pending", history[1].State)
		last, err := repo.TranslationWork.TargetHistory(ctx, held.UUID, history[1].Revision, 2)
		require.NoError(t, err)
		require.Len(t, last, 1)
		require.Equal(t, completed.EvidenceUUID, last[0].EvidenceUUID)
		return nil
	}))
}

func TestTranslationWorkCachesOutcomesWithoutInventingTranslations(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "cache-outcomes"}, "")
	now := time.Now().UTC()
	english := retainTranslationRequest(t, repo, "  Already English  ")
	blank := retainTranslationRequest(t, repo, " \n ")
	for _, item := range []struct {
		request *models.TranslationRequest
		cache   models.TranslationCacheInput
	}{
		{english, models.TranslationCacheInput{RequestUUID: english.UUID, Status: "unchanged", TranslatedText: &english.OriginalText, SourceLanguage: translationPointer("en-US"), Provider: translationPointer("translate-shell/bing"), Origin: "migration"}},
		{blank, models.TranslationCacheInput{RequestUUID: blank.UUID, Status: "no_text", Origin: "migration"}},
	} {
		cache := retainTranslationCache(t, repo, item.cache)
		target := retainTranslationTarget(t, repo, models.TranslationTargetInput{RequestUUID: item.request.UUID, PostUUID: post.UUID, Field: "caption", Origin: "migration"}, models.TranslationTargetSchedule{State: "pending"}, now)
		completed, err := publishTranslationTarget(repo, target, now)
		require.NoError(t, err)
		require.Equal(t, "completed", completed.State)
		require.Equal(t, item.cache.Status == "no_text", completed.EvidenceUUID == nil)
		require.Equal(t, item.cache.Status == "no_text", cache.TranslationUUID == nil)
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM translation_cache"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_translations"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_translation_evidence"))
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationWork.RetainCache(ctx, models.TranslationCacheInput{RequestUUID: english.UUID, Status: "translated", TranslatedText: translationPointer("A conflicting result"), Origin: "worker"})
		return err
	})
	require.ErrorIs(t, err, models.ErrTranslationWorkConflict)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_translations"), "rejected outcomes must not leave orphan results")
	for _, input := range []models.TranslationCacheInput{
		{RequestUUID: english.UUID, Status: "unchanged", TranslatedText: &english.OriginalText, Origin: "worker"},
		{RequestUUID: english.UUID, Status: "unchanged", TranslatedText: &english.OriginalText, SourceLanguage: translationPointer("ja"), Origin: "worker"},
		{RequestUUID: english.UUID, Status: "unchanged", TranslatedText: translationPointer(strings.TrimSpace(english.OriginalText)), SourceLanguage: translationPointer("en"), Origin: "worker"},
		{RequestUUID: english.UUID, Status: "no_text", TranslatedText: translationPointer(""), Origin: "worker"},
		{RequestUUID: english.UUID, Status: "translated", TranslatedText: translationPointer(" \n "), Origin: "worker"},
		{RequestUUID: english.UUID, Status: "translated", TranslatedText: translationPointer("text"), CapturedAt: "not-a-time", Origin: "worker"},
	} {
		_, _, err := archive.PrepareTranslationCache(english, input)
		require.ErrorIs(t, err, models.ErrTranslationWorkInvalid)
	}
	for _, input := range []models.TranslationRequestInput{
		{OriginalText: string([]byte{0xff}), TargetLanguage: "en", Policy: models.TranslationBingTextV1},
		{OriginalText: strings.Repeat("a", archive.MaxTranslationTextBytes+1), TargetLanguage: "en", Policy: models.TranslationBingTextV1},
		{TargetLanguage: "--help", Policy: models.TranslationBingTextV1},
		{TargetLanguage: "en", Policy: "unversioned-provider"},
	} {
		_, err := archive.PrepareTranslationRequest(input)
		require.ErrorIs(t, err, models.ErrTranslationWorkInvalid)
	}
	anonymousPath := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(db, anonymousPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	anonRaw := openRawDB(t, anonymousPath)
	defer anonRaw.Close()
	for _, table := range []string{"translation_target_history", "translation_targets", "translation_cache", "translation_requests", "source_translation_evidence", "source_translations"} {
		require.Zero(t, queryUint(t, anonRaw, "SELECT count(*) FROM "+table))
	}
	contents, err := os.ReadFile(anonymousPath)
	require.NoError(t, err)
	require.NotContains(t, string(contents), english.OriginalText)
	check := sqlite.NewDatabase()
	require.NoError(t, check.Open(anonymousPath))
	require.NoError(t, check.Close())
}

func TestTranslationWorkAtomicPublicationAndForgottenPost(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "atomic-translation"}, "")
	request := retainTranslationRequest(t, repo, "Original work")
	input := models.TranslationCacheInput{RequestUUID: request.UUID, Status: "translated", TranslatedText: translationPointer("Retained result"), Origin: "worker"}
	_, err := repo.TranslationWork.RetainCache(t.Context(), input)
	require.Error(t, err, "writes require a managed transaction")
	attachmentSQL(t, db, "CREATE TRIGGER reject_translation_cache BEFORE INSERT ON translation_cache BEGIN SELECT RAISE(ABORT,'fixture cache failure'); END")
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationWork.RetainCache(ctx, input)
		require.ErrorContains(t, err, "fixture cache failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrTranslationWorkAtomic)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_translations"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM translation_cache"))
	attachmentSQL(t, db, "DROP TRIGGER reject_translation_cache")
	retainTranslationCache(t, repo, input)
	now := time.Now().UTC()
	target := retainTranslationTarget(t, repo, models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: post.UUID, Field: "title", Origin: "migration"}, models.TranslationTargetSchedule{State: "pending"}, now)
	attachmentSQL(t, db, "CREATE TRIGGER reject_translation_publish BEFORE UPDATE ON translation_targets BEGIN SELECT RAISE(ABORT,'fixture publication failure'); END")
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationWork.PublishTarget(ctx, target.UUID, target.Revision, now)
		require.ErrorContains(t, err, "fixture publication failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrTranslationWorkAtomic)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_translation_evidence"))
	require.Equal(t, 1, postLinkRevision(t, repo, post.UUID))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM translation_target_history"))
	attachmentSQL(t, db, "DROP TRIGGER reject_translation_publish")
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", post.UUID)
	require.NoError(t, err)
	review, err := publishTranslationTarget(repo, target, now)
	require.NoError(t, err)
	require.Equal(t, "review", review.State)
	require.Equal(t, "post_forgotten", review.Reason)
	require.Nil(t, review.EvidenceUUID)
	repeated, err := publishTranslationTarget(repo, review, now)
	require.NoError(t, err)
	require.Equal(t, review, repeated)
	_, err = publishTranslationTarget(repo, target, now)
	require.ErrorIs(t, err, models.ErrTranslationWorkConflict, "stale revisions cannot publish")
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationWork.ScheduleTarget(ctx, review.UUID, review.Revision, models.TranslationTargetSchedule{State: "pending"}, now)
		return err
	})
	require.ErrorIs(t, err, models.ErrTranslationWorkConflict, "terminal review cannot reactivate a forgotten post")
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		input := target.TranslationTargetInput
		input.Field = "caption"
		_, err := repo.TranslationWork.RetainTarget(ctx, input, models.TranslationTargetSchedule{State: "pending"}, now)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourcePostForgotten)
	require.Equal(t, review, retainTranslationTarget(t, repo, target.TranslationTargetInput, models.TranslationTargetSchedule{State: "pending"}, now))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestTranslationWorkStartupRejectsCorruptStateWithoutWrites(t *testing.T) {
	for _, item := range []struct{ name, trigger, mutation string }{
		{"request", "translation_request_immutable", "UPDATE translation_requests SET original_text='Changed original'"},
		{"cache", "translation_cache_immutable", "UPDATE translation_cache SET uuid='11111111-1111-5111-8111-111111111111'"},
		{"target", "translation_target_identity", "UPDATE translation_targets SET field='caption',revision=revision+1"},
		{"history", "translation_target_history_immutable", "UPDATE translation_target_history SET priority=10"},
	} {
		t.Run(item.name, func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "corrupt-translation"}, "")
			request := retainTranslationRequest(t, repo, "Stored original")
			retainTranslationCache(t, repo, models.TranslationCacheInput{RequestUUID: request.UUID, Status: "translated", TranslatedText: translationPointer("Stored translation"), Origin: "worker"})
			retainTranslationTarget(t, repo, models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: post.UUID, Field: "title", Origin: "capture"}, models.TranslationTargetSchedule{State: "pending"}, time.Now().UTC())
			path := db.DatabasePath()
			require.NoError(t, db.Close())
			raw := openRawDB(t, path)
			var guard string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", item.trigger).Scan(&guard))
			_, err := raw.Exec("DROP TRIGGER " + item.trigger + ";" + item.mutation + ";" + guard)
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

func TestTranslationWorkBoundedLookupsUseIndexes(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, item := range []struct{ query, index string }{
		{"SELECT * FROM translation_targets WHERE request_uuid=? AND uuid>? ORDER BY uuid LIMIT 10", "translation_targets_request"},
		{"SELECT * FROM translation_targets WHERE post_uuid=? AND uuid>? ORDER BY uuid LIMIT 10", "translation_targets_post"},
		{"SELECT * FROM translation_targets WHERE request_uuid=? AND uuid>? AND state='pending' ORDER BY uuid LIMIT 10", "translation_targets_request_state"},
		{"SELECT * FROM translation_targets WHERE post_uuid=? AND uuid>? AND state='pending' ORDER BY uuid LIMIT 10", "translation_targets_post_state"},
	} {
		var id, parent, unused int
		var plan string
		require.NoError(t, raw.QueryRow("EXPLAIN QUERY PLAN "+item.query, uuid.NewString(), "").Scan(&id, &parent, &unused, &plan))
		require.Contains(t, plan, item.index)
		require.NotContains(t, plan, "SCAN ")
	}
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, query := range []models.TranslationTargetQuery{
			{}, {RequestUUID: uuid.NewString(), PostUUID: uuid.NewString()}, {PostUUID: "invalid"}, {PostUUID: uuid.NewString(), State: "invalid"},
			{PostUUID: uuid.NewString(), After: "invalid"}, {PostUUID: uuid.NewString(), Limit: 101},
		} {
			_, err := repo.TranslationWork.Targets(ctx, query)
			require.ErrorIs(t, err, models.ErrTranslationWorkInvalid)
		}
		_, err := repo.TranslationWork.TargetHistory(ctx, uuid.NewString(), -1, 10)
		require.ErrorIs(t, err, models.ErrTranslationWorkInvalid)
		return nil
	}))
}

func TestTranslationWorkMigrationPreservesLibraryAndRollsBackCollision(t *testing.T) {
	config.InitializeEmpty()
	for _, collision := range []bool{false, true} {
		name := "upgrade"
		if collision {
			name = "collision"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schema43.sqlite")
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
			for v := m.CurrentSchemaVersion(); v < sqlite.NativeSchemaBaseline+43; v = m.CurrentSchemaVersion() {
				require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(v)))
			}
			m.Close()
			before := map[string][][]any{}
			for _, table := range []string{"performers", "performer_names", "scenes", "images", "files", "archive_entities", "metadata_field_decisions", "archive_jobs", "source_translations"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE translation_targets(retained TEXT); INSERT INTO translation_targets VALUES('Original unrelated data')")
				require.NoError(t, err)
				require.Error(t, db.RunAllMigrations())
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty=1"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name IN ('translation_requests','translation_cache','translation_target_history')"))
				var retained string
				require.NoError(t, raw.QueryRow("SELECT retained FROM translation_targets").Scan(&retained))
				require.Equal(t, "Original unrelated data", retained)
				check := sqlite.NewDatabase()
				require.Error(t, check.Open(path))
				require.NoError(t, check.Close())
			} else {
				require.NoError(t, db.RunAllMigrations())
				require.NoError(t, db.ReInitialise())
				require.Equal(t, sqlite.GetRequiredSchemaVersion(), db.Version())
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000044"))
				for _, table := range []string{"translation_requests", "translation_cache", "translation_targets", "translation_target_history"} {
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
