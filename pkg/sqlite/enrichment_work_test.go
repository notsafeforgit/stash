package sqlite_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func enrichmentInput(t *testing.T, repo models.Repository) (models.EnrichmentTargetInput, *models.SourceCollection) {
	t.Helper()
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "123"}, "")
	url, err := observePostURL(repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: "https://x.com/source/status/123"})
	require.NoError(t, err)
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Source feed", Kind: "feed", Namespace: "native:twitter", State: "active", TargetURL: "https://x.com/source"}})
	return models.EnrichmentTargetInput{PostUUID: post.UUID, URLUUID: url.URLUUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
		Policy: models.EnrichmentGalleryMetadataV1, Origin: "migration"}, collection
}

func enrichmentCaptureInput(t *testing.T, post, origin string) models.SourceCaptureInput {
	t.Helper()
	payload, err := archive.PrepareRetainedCapture(origin, "twitter", []byte(`{"category":"twitter","tweet_id":"123","content":"Shared source text","num":1}`))
	require.NoError(t, err)
	return models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post, Origin: origin, Platform: "twitter", CapturedAt: time.Now().UTC(),
		RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload}
}

func enrichmentCapture(t *testing.T, repo models.Repository, target models.EnrichmentTargetInput, origin string) string {
	t.Helper()
	input := enrichmentCaptureInput(t, target.PostUUID, origin)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if _, err := repo.SourceEvidence.RecordCapture(ctx, input); err != nil {
			return err
		}
		return repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CaptureUUID: input.UUID, CollectionUUID: target.CollectionUUID, CollectionRevision: target.CollectionRevision})
	}))
	return input.UUID
}

func retainEnrichment(t *testing.T, repo models.Repository, input models.EnrichmentTargetInput, schedule models.EnrichmentSchedule, now time.Time) *models.EnrichmentTarget {
	t.Helper()
	var result *models.EnrichmentTarget
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = repo.EnrichmentWork.RetainTarget(ctx, input, schedule, now)
		return err
	}))
	return result
}

func scheduleEnrichment(repo models.Repository, target *models.EnrichmentTarget, schedule models.EnrichmentSchedule, now time.Time) (*models.EnrichmentTarget, error) {
	var result *models.EnrichmentTarget
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		result, err = repo.EnrichmentWork.Schedule(ctx, target.UUID, target.Revision, schedule, now)
		return err
	})
	return result, err
}

func completeEnrichment(repo models.Repository, input models.EnrichmentCompletionInput, now time.Time) (*models.EnrichmentCompletion, error) {
	var result *models.EnrichmentCompletion
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		result, err = repo.EnrichmentWork.Complete(ctx, input, now)
		return err
	})
	return result, err
}

func TestEnrichmentWorkPreservesChoicesAndCompletionAcrossRestore(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "galleries", "performers", "metadata_field_decisions", "archive_jobs"} {
		before[table] = albumJobRows(t, raw, table)
	}
	input, collection := enrichmentInput(t, repo)
	now := time.Date(2026, 10, 2, 2, 3, 4, 123456789, time.FixedZone("worker", -7*3600))
	schedule := models.EnrichmentSchedule{State: "held", Priority: 35, NotBefore: now.Add(time.Hour), Reason: "migration_review"}
	held := retainEnrichment(t, repo, input, schedule, now)
	input.Origin = "review"
	require.Equal(t, held, retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending", Priority: 100}, now.Add(time.Minute)))
	schedule.State, schedule.Reason = "excluded", "abandoned_originals"
	excluded, err := scheduleEnrichment(repo, held, schedule, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, excluded, retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending"}, now.Add(2*time.Minute)), "new observations cannot release an explicit exclusion")
	schedule.State, schedule.Reason = "review", "identity_conflict"
	review, err := scheduleEnrichment(repo, excluded, schedule, now.Add(2*time.Minute))
	require.NoError(t, err)
	schedule.State, schedule.Reason = "pending", ""
	pending, err := scheduleEnrichment(repo, review, schedule, now.Add(3*time.Minute))
	require.NoError(t, err)
	require.Equal(t, held.NotBefore, pending.NotBefore)
	require.Equal(t, held.Priority, pending.Priority)
	_, err = scheduleEnrichment(repo, held, schedule, now.Add(4*time.Minute))
	require.ErrorIs(t, err, models.ErrEnrichmentConflict, "a stale review cannot reset a newer choice")
	unchanged, err := scheduleEnrichment(repo, pending, schedule, now.Add(4*time.Minute))
	require.NoError(t, err)
	require.Equal(t, pending, unchanged)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		early, err := repo.EnrichmentWork.Ready(ctx, collection.UUID, pending.NotBefore.Add(-time.Nanosecond), 1)
		require.NoError(t, err)
		require.Empty(t, early)
		ready, err := repo.EnrichmentWork.Ready(ctx, collection.UUID, pending.NotBefore, 1)
		require.NoError(t, err)
		require.Equal(t, []models.EnrichmentTarget{*pending}, ready)
		return nil
	}))
	a := enrichmentCapture(t, repo, input, "gallery-dl")
	b := enrichmentCapture(t, repo, input, "gallery-dl-enrichment")
	completionInput := models.EnrichmentCompletionInput{UUID: uuid.NewString(), TargetUUID: pending.UUID, ExpectedRevision: pending.Revision, CaptureUUIDs: []string{a, b}}
	_, err = completeEnrichment(repo, completionInput, now.Add(time.Minute))
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	postRevision := postLinkRevision(t, repo, input.PostUUID)
	completion, err := completeEnrichment(repo, completionInput, pending.NotBefore)
	require.NoError(t, err)
	require.Equal(t, postRevision, postLinkRevision(t, repo, input.PostUUID), "completing work does not rewrite source evidence or selected metadata")
	completionInput.CaptureUUIDs = []string{b, a}
	replay, err := completeEnrichment(repo, completionInput, pending.NotBefore.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, completion, replay)
	changed := completionInput
	changed.CaptureUUIDs = []string{a}
	_, err = completeEnrichment(repo, changed, pending.NotBefore.Add(time.Minute))
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	changed = completionInput
	changed.UUID = uuid.NewString()
	_, err = completeEnrichment(repo, changed, pending.NotBefore.Add(time.Minute))
	require.ErrorIs(t, err, models.ErrEnrichmentConflict, "a second operation cannot relabel existing completion")
	var completed *models.EnrichmentTarget
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		completed, err = repo.EnrichmentWork.Target(ctx, pending.UUID)
		return err
	}))
	require.Equal(t, "completed", completed.State)
	require.Equal(t, pending.Revision+1, completed.Revision)
	require.Equal(t, completion.UUID, *completed.CompletionUUID)
	_, err = scheduleEnrichment(repo, completed, schedule, pending.NotBefore.Add(time.Minute))
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	definition := collection.SourceCollectionDefinition
	definition.State = "retired"
	putSourceCollection(t, repo, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", input.PostUUID)
	require.NoError(t, err)
	replay, err = completeEnrichment(repo, completionInput, pending.NotBefore.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, completion, replay, "historical receipts survive later collection edits and post deletion")
	require.Equal(t, completed, retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending"}, pending.NotBefore.Add(time.Hour)))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	backup := filepath.Join(t.TempDir(), "enrichment-backup.sqlite")
	require.NoError(t, db.Backup(backup))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(backup))
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		found, err := repo.EnrichmentWork.Completion(ctx, completion.UUID)
		require.NoError(t, err)
		require.Equal(t, completion, found)
		history, err := repo.EnrichmentWork.History(ctx, pending.UUID, 0, 3)
		require.NoError(t, err)
		require.Len(t, history, 3)
		require.Equal(t, []string{"held", "excluded", "review"}, []string{history[0].State, history[1].State, history[2].State})
		rest, err := repo.EnrichmentWork.History(ctx, pending.UUID, history[2].Revision, 3)
		require.NoError(t, err)
		require.Len(t, rest, 2)
		require.Equal(t, "pending", rest[0].State)
		require.Equal(t, "completed", rest[1].State)
		for _, q := range []models.EnrichmentTargetQuery{{PostUUID: input.PostUUID, State: "completed"}, {CollectionUUID: input.CollectionUUID}} {
			page, err := repo.EnrichmentWork.Targets(ctx, q)
			require.NoError(t, err)
			require.Equal(t, []models.EnrichmentTarget{*completed}, page)
			q.After = page[0].UUID
			page, err = repo.EnrichmentWork.Targets(ctx, q)
			require.NoError(t, err)
			require.Empty(t, page)
		}
		return nil
	}))
	anonymousPath := filepath.Join(t.TempDir(), "enrichment-anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(db, anonymousPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	anonRaw := openRawDB(t, anonymousPath)
	defer anonRaw.Close()
	for _, table := range []string{"enrichment_targets", "enrichment_target_history", "enrichment_completions", "enrichment_completion_captures"} {
		require.Zero(t, queryUint(t, anonRaw, "SELECT count(*) FROM "+table))
	}
	contents, err := os.ReadFile(anonymousPath)
	require.NoError(t, err)
	require.NotContains(t, string(contents), completed.URL)
	check := sqlite.NewDatabase()
	require.NoError(t, check.Open(anonymousPath))
	require.NoError(t, check.Close())
}

func TestEnrichmentWorkRejectsWrongEvidenceAndStaleBindings(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	input, collection := enrichmentInput(t, repo)
	now := time.Now().UTC()
	target := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending"}, now)
	correct := enrichmentCapture(t, repo, input, "gallery-dl")
	legacy := enrichmentCapture(t, repo, input, "legacy-nfo")
	otherPost := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "456"}, "")
	otherInput := input
	otherInput.PostUUID = otherPost.UUID
	wrongPost := enrichmentCapture(t, repo, otherInput, "gallery-dl")
	other := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Other feed", Kind: "feed", State: "active"}})
	otherInput = input
	otherInput.CollectionUUID, otherInput.CollectionRevision = other.UUID, other.Revision
	wrongCollection := enrichmentCapture(t, repo, otherInput, "gallery-dl")
	unbound := recordSourceTestCapture(t, repo, enrichmentCaptureInput(t, input.PostUUID, "gallery-dl"))
	for _, capture := range []string{legacy, wrongPost, wrongCollection, unbound.UUID, uuid.NewString()} {
		_, err := completeEnrichment(repo, models.EnrichmentCompletionInput{UUID: uuid.NewString(), TargetUUID: target.UUID, ExpectedRevision: target.Revision, CaptureUUIDs: []string{correct, capture}}, now)
		require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_completions"))
	definition := collection.SourceCollectionDefinition
	definition.Label = "Reviewed source binding"
	current := putSourceCollection(t, repo, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		ready, err := repo.EnrichmentWork.Ready(ctx, collection.UUID, now.Add(time.Hour), 10)
		require.NoError(t, err)
		require.Empty(t, ready, "old collection revisions cannot silently execute")
		return nil
	}))
	_, err := completeEnrichment(repo, models.EnrichmentCompletionInput{UUID: uuid.NewString(), TargetUUID: target.UUID, ExpectedRevision: target.Revision, CaptureUUIDs: []string{correct}}, now)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	held, err := scheduleEnrichment(repo, target, models.EnrichmentSchedule{State: "held", Reason: "source_changed"}, now)
	require.NoError(t, err)
	_, err = scheduleEnrichment(repo, held, models.EnrichmentSchedule{State: "pending"}, now)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	input.CollectionRevision = current.Revision
	newTarget := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending", Priority: 20}, now)
	require.NotEqual(t, target.UUID, newTarget.UUID)
	_, err = completeEnrichment(repo, models.EnrichmentCompletionInput{UUID: uuid.NewString(), TargetUUID: newTarget.UUID, ExpectedRevision: newTarget.Revision, CaptureUUIDs: []string{correct}}, now)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict, "old capture bindings do not certify a different source revision")
	currentCapture := enrichmentCapture(t, repo, input, "gallery-dl")
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", input.PostUUID)
	require.NoError(t, err)
	_, err = completeEnrichment(repo, models.EnrichmentCompletionInput{UUID: uuid.NewString(), TargetUUID: newTarget.UUID, ExpectedRevision: newTarget.Revision, CaptureUUIDs: []string{currentCapture}}, now)
	require.ErrorIs(t, err, models.ErrSourcePostForgotten)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		ready, err := repo.EnrichmentWork.Ready(ctx, collection.UUID, now.Add(time.Hour), 10)
		require.NoError(t, err)
		require.Empty(t, ready)
		return nil
	}))
	_, err = scheduleEnrichment(repo, newTarget, models.EnrichmentSchedule{State: "review", Reason: "post_forgotten"}, now)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestEnrichmentWorkCompletionRollsBackWholeCaptureTransaction(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	input, _ := enrichmentInput(t, repo)
	now := time.Now().UTC()
	_, err := repo.EnrichmentWork.RetainTarget(t.Context(), input, models.EnrichmentSchedule{State: "pending"}, now)
	require.Error(t, err, "writes require a managed transaction")
	target := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending"}, now)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	postRevision := postLinkRevision(t, repo, input.PostUUID)
	attachmentSQL(t, db, "CREATE TRIGGER reject_enrichment_publish BEFORE UPDATE ON enrichment_targets BEGIN SELECT RAISE(ABORT,'fixture publication failure'); END")
	capture := enrichmentCaptureInput(t, input.PostUUID, "gallery-dl")
	completion := models.EnrichmentCompletionInput{UUID: uuid.NewString(), TargetUUID: target.UUID, ExpectedRevision: target.Revision, CaptureUUIDs: []string{capture.UUID}}
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceEvidence.RecordCapture(ctx, capture)
		require.NoError(t, err)
		require.NoError(t, repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CaptureUUID: capture.UUID, CollectionUUID: input.CollectionUUID, CollectionRevision: input.CollectionRevision}))
		_, err = repo.EnrichmentWork.Complete(ctx, completion, now)
		require.ErrorContains(t, err, "fixture publication failure")
		return nil // A caller swallowing a late failure must not commit its prefix.
	})
	require.ErrorIs(t, err, models.ErrEnrichmentAtomic)
	for _, table := range []string{"source_captures", "source_collection_captures", "enrichment_completions", "enrichment_completion_captures"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Equal(t, postRevision, postLinkRevision(t, repo, input.PostUUID))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_target_history"))
	attachmentSQL(t, db, "DROP TRIGGER reject_enrichment_publish")
	completion.CaptureUUIDs = []string{enrichmentCapture(t, repo, input, "gallery-dl")}
	_, err = completeEnrichment(repo, completion, now)
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO enrichment_completion_captures(completion_uuid,capture_uuid) VALUES(?,?)", completion.UUID, enrichmentCapture(t, repo, input, "gallery-dl"))
	require.ErrorContains(t, err, "scope", "evidence cannot be appended after completion")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestEnrichmentWorkStartupRejectsCorruptStateWithoutWrites(t *testing.T) {
	for _, item := range []struct{ name, trigger, mutation string }{
		{"completion_digest", "enrichment_completion_immutable", "UPDATE enrichment_completions SET request_digest='" + strings.Repeat("0", 64) + "'"},
		{"capture_count", "", "DELETE FROM enrichment_completion_captures"},
		{"history", "enrichment_target_history_immutable", "UPDATE enrichment_target_history SET priority=100 WHERE revision=1"},
		{"identity", "enrichment_target_identity", ""},
	} {
		t.Run(item.name, func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			input, _ := enrichmentInput(t, repo)
			now := time.Now().UTC()
			target := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending"}, now)
			if item.name == "identity" {
				url, err := observePostURL(repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(input.PostUUID), URL: "https://twitter.com/source/status/123"})
				require.NoError(t, err)
				item.mutation = "UPDATE enrichment_targets SET url_uuid='" + url.URLUUID + "'"
			} else {
				_, err := completeEnrichment(repo, models.EnrichmentCompletionInput{UUID: uuid.NewString(), TargetUUID: target.UUID, ExpectedRevision: target.Revision,
					CaptureUUIDs: []string{enrichmentCapture(t, repo, input, "gallery-dl")}}, now)
				require.NoError(t, err)
			}
			path := db.DatabasePath()
			require.NoError(t, db.Close())
			raw := openRawDB(t, path)
			var guards []string
			if item.trigger != "" {
				var guard string
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", item.trigger).Scan(&guard))
				guards = append(guards, guard)
				_, err := raw.Exec("DROP TRIGGER " + item.trigger)
				require.NoError(t, err)
			}
			if item.name == "identity" {
				var guard string
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='enrichment_target_history_update'").Scan(&guard))
				guards = append(guards, guard)
				_, err := raw.Exec("DROP TRIGGER enrichment_target_history_update")
				require.NoError(t, err)
			}
			_, err := raw.Exec(item.mutation)
			require.NoError(t, err)
			for _, guard := range guards {
				_, err = raw.Exec(guard)
				require.NoError(t, err)
			}
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

func TestEnrichmentWorkAdmissionAndBoundedQueries(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	input, collection := enrichmentInput(t, repo)
	now := time.Now().UTC()
	feed, err := observePostURL(repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(input.PostUUID), URL: "https://x.com/source"})
	require.NoError(t, err)
	otherPost := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "456"}, "")
	for _, change := range []func(*models.EnrichmentTargetInput){
		func(v *models.EnrichmentTargetInput) { v.URLUUID = feed.URLUUID },
		func(v *models.EnrichmentTargetInput) { v.PostUUID = otherPost.UUID },
		func(v *models.EnrichmentTargetInput) { v.CollectionRevision = 999 },
		func(v *models.EnrichmentTargetInput) { v.URLUUID = uuid.NewString() },
	} {
		invalid := input
		change(&invalid)
		err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.EnrichmentWork.RetainTarget(ctx, invalid, models.EnrichmentSchedule{State: "pending"}, now)
			return err
		})
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	}
	for _, schedule := range []models.EnrichmentSchedule{
		{State: "completed"}, {State: "excluded"}, {State: "review"}, {State: "invalid"}, {State: "held", Reason: "HTTP error: private source details"},
		{State: "pending", Priority: 101}, {State: "pending", Priority: -1}, {State: "held", Reason: strings.Repeat("x", 65)},
	} {
		err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.EnrichmentWork.RetainTarget(ctx, input, schedule, now)
			return err
		})
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	}
	definition := collection.SourceCollectionDefinition
	definition.State = "disabled"
	disabled := putSourceCollection(t, repo, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	input.CollectionRevision = disabled.Revision
	held := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "held"}, now)
	_, err = scheduleEnrichment(repo, held, models.EnrichmentSchedule{State: "pending"}, now)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, q := range []models.EnrichmentTargetQuery{
			{}, {PostUUID: input.PostUUID, CollectionUUID: input.CollectionUUID}, {PostUUID: "invalid"}, {CollectionUUID: "invalid"},
			{PostUUID: input.PostUUID, State: "invalid"}, {CollectionUUID: input.CollectionUUID, After: "invalid"}, {PostUUID: input.PostUUID, Limit: 101},
		} {
			_, err := repo.EnrichmentWork.Targets(ctx, q)
			require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
		}
		_, err := repo.EnrichmentWork.History(ctx, held.UUID, -1, 10)
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
		_, err = repo.EnrichmentWork.Ready(ctx, collection.UUID, time.Time{}, 10)
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
		_, err = repo.EnrichmentWork.Ready(ctx, collection.UUID, now, 101)
		require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, item := range []struct{ query, index string }{
		{"SELECT * FROM enrichment_targets WHERE post_uuid=? AND uuid>? ORDER BY uuid LIMIT 10", "enrichment_targets_post"},
		{"SELECT * FROM enrichment_targets WHERE collection_uuid=? AND uuid>? ORDER BY uuid LIMIT 10", "enrichment_targets_collection"},
		{"SELECT * FROM enrichment_targets WHERE post_uuid=? AND state='held' AND uuid>? ORDER BY uuid LIMIT 10", "enrichment_targets_post_state"},
		{"SELECT * FROM enrichment_targets WHERE collection_uuid=? AND state='held' AND uuid>? ORDER BY uuid LIMIT 10", "enrichment_targets_collection_state"},
		{"SELECT * FROM enrichment_targets WHERE collection_uuid=? AND state='pending' AND not_before<=? ORDER BY priority DESC,not_before,uuid LIMIT 10", "enrichment_targets_ready"},
	} {
		var id, parent, unused int
		var plan string
		require.NoError(t, raw.QueryRow("EXPLAIN QUERY PLAN "+item.query, uuid.NewString(), "").Scan(&id, &parent, &unused, &plan))
		require.Contains(t, plan, item.index)
		require.NotContains(t, plan, "SCAN ")
	}
}

func TestEnrichmentWorkMigrationPreservesLibraryAndRollsBackCollision(t *testing.T) {
	for _, collision := range []bool{false, true} {
		name := "upgrade"
		if collision {
			name = "collision"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schema49.sqlite")
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
			for v := m.CurrentSchemaVersion(); v < sqlite.NativeSchemaBaseline+49; v = m.CurrentSchemaVersion() {
				require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(v)))
			}
			m.Close()
			before := map[string][][]any{}
			for _, table := range []string{"scenes", "images", "files", "performers", "performer_names", "archive_entities", "metadata_field_decisions", "translation_targets", "archive_jobs"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision {
				_, err = raw.Exec("CREATE TABLE enrichment_completions(retained TEXT); INSERT INTO enrichment_completions VALUES('Original unrelated data')")
				require.NoError(t, err)
				require.Error(t, db.RunAllMigrations())
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty=1"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name IN ('enrichment_targets','enrichment_target_history','enrichment_completion_captures')"))
				var retained string
				require.NoError(t, raw.QueryRow("SELECT retained FROM enrichment_completions").Scan(&retained))
				require.Equal(t, "Original unrelated data", retained)
			} else {
				require.NoError(t, db.RunAllMigrations())
				require.NoError(t, db.ReInitialise())
				require.Equal(t, sqlite.GetRequiredSchemaVersion(), db.Version())
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000050"))
				for _, table := range []string{"enrichment_targets", "enrichment_target_history", "enrichment_completions", "enrichment_completion_captures"} {
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
