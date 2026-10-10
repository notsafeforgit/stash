package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/translation"
	"github.com/stretchr/testify/require"
)

func translationPolicyDefinition() models.TranslationPolicyDefinition {
	return models.TranslationPolicyDefinition{Enabled: true, ProviderPolicy: models.TranslationBingTextV1, TargetLanguage: "en", Title: true, Caption: true, Priority: 100}
}

func putTranslationPolicy(t *testing.T, repo models.Repository, collection *models.SourceCollection, expected int, definition models.TranslationPolicyDefinition) *models.TranslationPolicy {
	t.Helper()
	var ret *models.TranslationPolicy
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.TranslationPolicy.Put(ctx, models.TranslationPolicyInput{CollectionUUID: collection.UUID, ExpectedRevision: expected,
			ExpectedCollectionRevision: collection.Revision, Definition: definition, Origin: "review", Reason: "Translate captured source text"})
		return err
	}))
	return ret
}

func translationPolicyCapture(t *testing.T, repo models.Repository, collection *models.SourceCollection, post string, title, caption *string) models.CollectionCapture {
	t.Helper()
	input := sourceTestCapture(t, post, 1, "Shared profile")
	input.Metadata.Title, input.Metadata.OriginalText = title, caption
	capture := recordSourceTestCapture(t, repo, input)
	scope := models.CollectionCapture{CaptureUUID: capture.UUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.SourceCollection.RecordCapture(ctx, scope) }))
	return scope
}

func scheduleTranslationCapture(t *testing.T, repo models.Repository, input models.CollectionCapture, now time.Time) *models.CaptureTranslationDecision {
	t.Helper()
	var ret *models.CaptureTranslationDecision
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.TranslationPolicy.ScheduleCapture(ctx, input, now)
		return err
	}))
	return ret
}

func translationPolicyCollection(t *testing.T, repo models.Repository) *models.SourceCollection {
	t.Helper()
	return putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Captured source", Kind: "feed", State: "active"}})
}

func TestTranslationPolicySchedulesSharedTextAndPreservesChoicesAcrossRestore(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "metadata_field_decisions"} {
		before[table] = albumJobRows(t, raw, table)
	}
	collection := translationPolicyCollection(t, repo)
	policy := putTranslationPolicy(t, repo, collection, 0, translationPolicyDefinition())
	require.Equal(t, policy, putTranslationPolicy(t, repo, collection, policy.Revision, policy.Definition))
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "policy-post"}, "")
	text := "  原文\n<&>  "
	s, clock := translationService(t, repo)
	first := translationPolicyCapture(t, repo, collection, post.UUID, &text, &text)
	decision := scheduleTranslationCapture(t, repo, first, s.Durable.Now())
	require.Equal(t, "recorded", decision.Status)
	require.Equal(t, policy.Revision, *decision.PolicyRevision)
	require.Len(t, decision.Entries, 2)
	for _, entry := range decision.Entries {
		require.Equal(t, "created", entry.Status)
		target := translationTarget(t, repo, *entry.TargetUUID)
		require.Equal(t, "pending", target.State)
		require.Equal(t, 100, target.Priority)
	}
	var held *models.TranslationTarget
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		held, err = repo.TranslationWork.ScheduleTarget(ctx, *decision.Entries[0].TargetUUID, 1,
			models.TranslationTargetSchedule{State: "held", Priority: 20, NotBefore: s.Durable.Now().Add(time.Hour)}, s.Durable.Now())
		return err
	}))
	second := translationPolicyCapture(t, repo, collection, post.UUID, &text, &text)
	repeated := scheduleTranslationCapture(t, repo, second, s.Durable.Now())
	for _, entry := range repeated.Entries {
		require.Equal(t, "retained", entry.Status)
	}
	require.Equal(t, held, translationTarget(t, repo, held.UUID))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM translation_requests"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM translation_targets"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM archive_jobs WHERE kind='text.translate'"))
	calls := 0
	worker := translation.NewWorker(s, translation.ProviderFunc(func(_ context.Context, request models.TranslationRequest) (models.TranslationCacheInput, error) {
		calls++
		require.Equal(t, text, request.OriginalText)
		return translationOutput(), nil
	}))
	processTranslation(t, worker)
	require.Equal(t, 1, calls)
	require.Equal(t, held, translationTarget(t, repo, held.UUID))
	require.Equal(t, "completed", translationTarget(t, repo, *decision.Entries[1].TargetUUID).State)
	definition := policy.Definition
	definition.Enabled = false
	putTranslationPolicy(t, repo, collection, policy.Revision, definition)
	clock.Add(time.Minute.Milliseconds())
	require.Equal(t, decision, scheduleTranslationCapture(t, repo, first, s.Durable.Now()), "original capture decision survives completion and later policy edits")
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	backup := filepath.Join(t.TempDir(), "translation-policy-backup.sqlite")
	require.NoError(t, db.Backup(backup))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(backup))
	repo = db.Repository()
	require.Equal(t, decision, scheduleTranslationCapture(t, repo, first, s.Durable.Now()))
	anonPath := filepath.Join(t.TempDir(), "translation-policy-anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(db, anonPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	anon := openRawDB(t, anonPath)
	defer anon.Close()
	for _, table := range []string{"translation_policies", "translation_policy_revisions", "capture_translation_decisions", "capture_translation_entries"} {
		require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestTranslationPolicyFreezesDisabledDecisionsAndReviewsChangedCollections(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	collection := translationPolicyCollection(t, repo)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "policy-disabled"}, "")
	text, now := "Source title", time.Now().UTC()
	first := translationPolicyCapture(t, repo, collection, post.UUID, &text, nil)
	missing := scheduleTranslationCapture(t, repo, first, now)
	require.Equal(t, "no_policy", missing.Status)
	definition := translationPolicyDefinition()
	definition.Enabled = false
	policy := putTranslationPolicy(t, repo, collection, 0, definition)
	second := translationPolicyCapture(t, repo, collection, post.UUID, &text, nil)
	disabled := scheduleTranslationCapture(t, repo, second, now)
	require.Equal(t, "disabled", disabled.Status)
	definition.Enabled = true
	policy = putTranslationPolicy(t, repo, collection, policy.Revision, definition)
	require.Equal(t, missing, scheduleTranslationCapture(t, repo, first, now.Add(time.Hour)))
	require.Equal(t, disabled, scheduleTranslationCapture(t, repo, second, now.Add(time.Hour)))
	old := *collection
	changed := collection.SourceCollectionDefinition
	changed.Label = "New scope"
	collection = putSourceCollection(t, repo, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, SourceCollectionDefinition: changed, Origin: "review"})
	for _, scope := range []*models.SourceCollection{&old, collection} {
		capture := translationPolicyCapture(t, repo, scope, post.UUID, &text, nil)
		require.Equal(t, "collection_changed", scheduleTranslationCapture(t, repo, capture, now).Status)
	}
	putTranslationPolicy(t, repo, collection, policy.Revision, definition)
	third := translationPolicyCapture(t, repo, collection, post.UUID, &text, nil)
	result := scheduleTranslationCapture(t, repo, third, now)
	require.Equal(t, "recorded", result.Status)
	require.Equal(t, "no_text", result.Entries[0].Status)
	require.Equal(t, "created", result.Entries[1].Status)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestTranslationPolicyMissingLargeAndDistinctSourceText(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	collection := translationPolicyCollection(t, repo)
	putTranslationPolicy(t, repo, collection, 0, translationPolicyDefinition())
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "policy-text"}, "")
	large, space, text := strings.Repeat("x", 200<<10), " \n\t\u3000", "Exact source"
	now := time.Now().UTC()
	first := translationPolicyCapture(t, repo, collection, post.UUID, &large, &space)
	decision := scheduleTranslationCapture(t, repo, first, now)
	require.Equal(t, "no_text", decision.Entries[0].Status)
	require.Equal(t, "created", decision.Entries[1].Status)
	second := translationPolicyCapture(t, repo, collection, post.UUID, &text, nil)
	one := scheduleTranslationCapture(t, repo, second, now)
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "policy-text"}, "")
	third := translationPolicyCapture(t, repo, collection, other.UUID, &text, nil)
	two := scheduleTranslationCapture(t, repo, third, now)
	a, b := translationTarget(t, repo, *one.Entries[1].TargetUUID), translationTarget(t, repo, *two.Entries[1].TargetUUID)
	require.NotEqual(t, a.UUID, b.UUID)
	require.Equal(t, a.RequestUUID, b.RequestUUID, "different posts share translation requests, not their target identity")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestTranslationPolicyLateFailureCannotLeavePartialWork(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	collection := translationPolicyCollection(t, repo)
	putTranslationPolicy(t, repo, collection, 0, translationPolicyDefinition())
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "policy-atomic"}, "")
	text := "New work"
	scope := translationPolicyCapture(t, repo, collection, post.UUID, &text, &text)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER reject_capture_translation BEFORE INSERT ON capture_translation_entries WHEN NEW.field='title'
 BEGIN SELECT RAISE(ABORT,'fixture late failure'); END`)
	require.NoError(t, err)
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationPolicy.ScheduleCapture(ctx, scope, time.Now())
		require.ErrorContains(t, err, "fixture late failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrTranslationWorkAtomic)
	for _, table := range []string{"translation_requests", "translation_targets", "translation_target_history", "capture_translation_decisions", "capture_translation_entries"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	_, err = raw.Exec("DROP TRIGGER reject_capture_translation")
	require.NoError(t, err)
	require.Equal(t, "recorded", scheduleTranslationCapture(t, repo, scope, time.Now()).Status)
}

func TestTranslationPolicyFailedRevisionCannotLeaveAnUnfinishedPolicy(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	collection := translationPolicyCollection(t, repo)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER reject_translation_policy BEFORE INSERT ON translation_policy_revisions
 BEGIN SELECT RAISE(ABORT,'fixture policy failure'); END`)
	require.NoError(t, err)
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationPolicy.Put(ctx, models.TranslationPolicyInput{CollectionUUID: collection.UUID, ExpectedCollectionRevision: collection.Revision,
			Origin: "review", Definition: translationPolicyDefinition()})
		require.ErrorContains(t, err, "fixture policy failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrTranslationWorkAtomic)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM translation_policies"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM translation_policy_revisions"))
	_, err = raw.Exec("DROP TRIGGER reject_translation_policy")
	require.NoError(t, err)
	putTranslationPolicy(t, repo, collection, 0, translationPolicyDefinition())
}

func TestTranslationPolicyRejectsStaleInvalidAndUnboundWork(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	collection := translationPolicyCollection(t, repo)
	input := models.TranslationPolicyInput{CollectionUUID: collection.UUID, ExpectedCollectionRevision: collection.Revision, Definition: translationPolicyDefinition(), Origin: "review"}
	putTranslationPolicy(t, repo, collection, 0, input.Definition)
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationPolicy.Put(ctx, input)
		return err
	}), models.ErrTranslationPolicyConflict)
	for _, change := range []func(*models.TranslationPolicyInput){
		func(i *models.TranslationPolicyInput) { i.Definition.Priority = 101 },
		func(i *models.TranslationPolicyInput) { i.Definition.TargetLanguage = "en;bad" },
		func(i *models.TranslationPolicyInput) { i.Definition.ProviderPolicy = "unknown" },
		func(i *models.TranslationPolicyInput) { i.Definition.Title, i.Definition.Caption = false, false },
		func(i *models.TranslationPolicyInput) { i.Origin = "producer" },
	} {
		bad := input
		bad.ExpectedRevision = 1
		change(&bad)
		require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.TranslationPolicy.Put(ctx, bad)
			return err
		}), models.ErrTranslationPolicyInvalid)
	}
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "policy-unbound"}, "")
	capture := recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 1, "profile"))
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.TranslationPolicy.ScheduleCapture(ctx, models.CollectionCapture{CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, CaptureUUID: capture.UUID}, time.Now())
		return err
	}), models.ErrTranslationPolicyInvalid)
}

func TestTranslationPolicyAuditRejectsCorruptDecisionsWithoutWrites(t *testing.T) {
	for _, item := range []struct{ trigger, mutation string }{
		{"translation_policy_revision_immutable", "UPDATE translation_policy_revisions SET definition=json_set(definition,'$.target_language','fr')"},
		{"capture_translation_entry_immutable", "UPDATE capture_translation_entries SET field='caption' WHERE field='title'"},
		{"capture_translation_decision_immutable", "UPDATE capture_translation_decisions SET entry_count=2"},
	} {
		t.Run(item.trigger, func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			collection := translationPolicyCollection(t, repo)
			definition := translationPolicyDefinition()
			definition.Caption = false
			putTranslationPolicy(t, repo, collection, 0, definition)
			post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "policy-corrupt"}, "")
			text := "Original text"
			scope := translationPolicyCapture(t, repo, collection, post.UUID, &text, nil)
			scheduleTranslationCapture(t, repo, scope, time.Now())
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
			require.Error(t, check.AuditForTesting(path))
			require.NoError(t, check.Close())
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
