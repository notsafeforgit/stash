package sqlite_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func previewEnrichmentRebind(t *testing.T, repo models.Repository, input models.EnrichmentRebindInput) *models.EnrichmentRebindPlan {
	t.Helper()
	var plan *models.EnrichmentRebindPlan
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		plan, err = repo.EnrichmentWork.PreviewRebind(ctx, input)
		return err
	}))
	return plan
}

func applyEnrichmentRebind(repo models.Repository, plan *models.EnrichmentRebindPlan, now time.Time) (*models.EnrichmentRebinding, error) {
	var receipt *models.EnrichmentRebinding
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		receipt, err = repo.EnrichmentWork.Rebind(ctx, plan.Input, plan.PlanSHA256, now)
		return err
	})
	return receipt, err
}

func changedEnrichmentCollection(t *testing.T, repo models.Repository, collection *models.SourceCollection) *models.SourceCollection {
	t.Helper()
	definition := collection.SourceCollectionDefinition
	definition.Label += " revised"
	result, err := reviseEnrichmentCollection(repo, collection.UUID, collection.Revision, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: definition})
	require.NoError(t, err)
	return result
}

func enrichmentRebindInput(collection *models.SourceCollection, targets ...*models.EnrichmentTarget) models.EnrichmentRebindInput {
	input := models.EnrichmentRebindInput{UUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, Reason: "Reviewed the collection's current settings"}
	for _, target := range targets {
		input.Targets = append(input.Targets, models.EnrichmentTargetRef{TargetUUID: target.UUID, Revision: target.Revision})
	}
	return input
}

func TestEnrichmentRebindPreservesSchedulesHistoryAndRestores(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	input, collection := enrichmentInput(t, repo)
	now := time.Now().UTC()
	first := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending", Priority: 7}, now)
	url, err := observePostURL(repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(input.PostUUID), URL: "https://twitter.com/source/status/123"})
	require.NoError(t, err)
	collection = changedEnrichmentCollection(t, repo, collection)
	input.URLUUID, input.CollectionRevision = url.URLUUID, collection.Revision
	second := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending", Priority: 99, NotBefore: now.Add(time.Hour)}, now)
	collection = changedEnrichmentCollection(t, repo, collection)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	unchanged := map[string][][]any{}
	for _, table := range []string{"source_posts", "source_post_urls", "source_collection_revisions", "source_payloads", "scenes", "images", "galleries", "performers", "metadata_field_decisions", "archive_jobs"} {
		unchanged[table] = albumJobRows(t, raw, table)
	}
	plan := previewEnrichmentRebind(t, repo, enrichmentRebindInput(collection, second, first))
	body, err := json.Marshal(plan)
	require.NoError(t, err)
	decoded, err := archive.DecodeEnrichmentRebindPlan(body)
	require.NoError(t, err)
	require.Equal(t, plan, decoded)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_activations"))
	require.Equal(t, first, readEnrichmentTarget(t, repo, first.UUID), "preview has no hold or other writes")
	receipt, err := applyEnrichmentRebind(repo, plan, now.Add(time.Minute))
	require.NoError(t, err)
	for _, entry := range plan.Activation.Entries {
		old := readEnrichmentTarget(t, repo, entry.TargetUUID)
		require.Equal(t, entry.Revision+1, old.Revision)
		require.Equal(t, "excluded", old.State)
		require.Equal(t, "activation_rebound", old.Reason)
		replacement := readEnrichmentTarget(t, repo, entry.ReleasedTargetUUID)
		require.Equal(t, "pending", replacement.State)
		require.Equal(t, 1, replacement.Revision)
		require.Equal(t, collection.Revision, replacement.CollectionRevision)
		require.Equal(t, old.Priority, replacement.Priority)
		require.True(t, old.NotBefore.Equal(replacement.NotBefore))
		var states string
		require.NoError(t, raw.QueryRow("SELECT group_concat(state,',') FROM (SELECT state FROM enrichment_target_history WHERE target_uuid=? ORDER BY revision)", old.UUID).Scan(&states))
		require.Equal(t, "pending,held,excluded", states)
	}
	for table, rows := range unchanged {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	_, err = raw.Exec("UPDATE enrichment_rebindings SET input_sha256=input_sha256")
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("UPDATE enrichment_rebinding_targets SET held_revision=held_revision")
	require.ErrorContains(t, err, "immutable")
	// Replay must recover the saved decision even after unrelated later changes.
	replacement := readEnrichmentTarget(t, repo, plan.Activation.Entries[0].ReleasedTargetUUID)
	_, err = scheduleEnrichment(repo, replacement, models.EnrichmentSchedule{State: "held", Priority: 31, NotBefore: now.Add(2 * time.Hour)}, now.Add(2*time.Minute))
	require.NoError(t, err)
	definition := collection.SourceCollectionDefinition
	definition.State = "retired"
	_, err = reviseEnrichmentCollection(repo, collection.UUID, collection.Revision, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: definition})
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten',revision=revision+1 WHERE uuid=?", input.PostUUID)
	require.NoError(t, err)
	backup := filepath.Join(t.TempDir(), "rebind.sqlite")
	require.NoError(t, db.Backup(backup))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(backup))
	repo = db.Repository()
	slices.Reverse(plan.Input.Targets)
	replayed, err := applyEnrichmentRebind(repo, plan, now.Add(3*time.Minute))
	require.NoError(t, err)
	require.Equal(t, receipt, replayed)
	changed := *plan
	changed.Input.Reason += " changed"
	_, err = applyEnrichmentRebind(repo, &changed, now.Add(3*time.Minute))
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	changed = *plan
	changed.PlanSHA256 = strings.Repeat("0", 64)
	_, err = applyEnrichmentRebind(repo, &changed, now.Add(3*time.Minute))
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	anonPath := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(db, anonPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	anon := openRawDB(t, anonPath)
	defer anon.Close()
	for _, table := range []string{"enrichment_rebindings", "enrichment_rebinding_targets", "enrichment_activations", "enrichment_targets"} {
		require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, anon, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestEnrichmentRebindRejectsStaleReviewsAndRollsBackAfterActivation(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	input, collection := enrichmentInput(t, repo)
	now := time.Now().UTC()
	target := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending", Priority: 17}, now)
	collection = changedEnrichmentCollection(t, repo, collection)
	request := enrichmentRebindInput(collection, target)
	plan := previewEnrichmentRebind(t, repo, request)
	_, err := observePostURL(repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(input.PostUUID), URL: "https://x.com/source/status/123?context=new"})
	require.NoError(t, err)
	_, err = applyEnrichmentRebind(repo, plan, now)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	plan = previewEnrichmentRebind(t, repo, request)
	collection = changedEnrichmentCollection(t, repo, collection)
	_, err = applyEnrichmentRebind(repo, plan, now)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	request.CollectionRevision = collection.Revision
	plan = previewEnrichmentRebind(t, repo, request)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec(`CREATE TRIGGER rebind_late_failure BEFORE INSERT ON enrichment_rebinding_targets BEGIN SELECT RAISE(ABORT,'Injected late rebind failure'); END`)
	require.NoError(t, err)
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.EnrichmentWork.Rebind(ctx, plan.Input, plan.PlanSHA256, now)
		require.ErrorContains(t, err, "Injected late rebind failure")
		return nil // Even a caller swallowing the error cannot commit a partial operation.
	}), models.ErrEnrichmentAtomic)
	require.Equal(t, target, readEnrichmentTarget(t, repo, target.UUID))
	for _, table := range []string{"enrichment_rebindings", "enrichment_rebinding_targets", "enrichment_activations", "enrichment_activation_targets", "archive_jobs"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_target_history"))
	_, err = raw.Exec("DROP TRIGGER rebind_late_failure")
	require.NoError(t, err)
	other := request
	other.UUID = uuid.NewString()
	competing := previewEnrichmentRebind(t, repo, other)
	_, err = applyEnrichmentRebind(repo, plan, now)
	require.NoError(t, err)
	_, err = applyEnrichmentRebind(repo, competing, now)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
}

func TestEnrichmentRebindCandidatesPageOldRevisionsAndRejectInvalidSelections(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	input, collection := enrichmentInput(t, repo)
	now := time.Now().UTC()
	first := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending"}, now)
	collection = changedEnrichmentCollection(t, repo, collection)
	input.CollectionRevision = collection.Revision
	second := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending"}, now)
	collection = changedEnrichmentCollection(t, repo, collection)
	read := func(revision int, cursor *models.EnrichmentRebindCursor, limit int) ([]models.EnrichmentRebindCandidate, error) {
		var rows []models.EnrichmentRebindCandidate
		err := repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			rows, err = repo.EnrichmentWork.RebindCandidates(ctx, collection.UUID, revision, cursor, limit)
			return err
		})
		return rows, err
	}
	rows, err := read(3, nil, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, first.UUID, rows[0].Target.UUID)
	require.Equal(t, "eligible", rows[0].Disposition)
	rows, err = read(3, &models.EnrichmentRebindCursor{CollectionRevision: 1, TargetUUID: first.UUID}, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, second.UUID, rows[0].Target.UUID)
	last, err := read(3, &models.EnrichmentRebindCursor{CollectionRevision: 2, TargetUUID: second.UUID}, 1)
	require.NoError(t, err)
	require.Empty(t, last)
	_, err = read(2, nil, 1)
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	_, err = read(3, nil, 101)
	require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	_, err = read(3, &models.EnrichmentRebindCursor{CollectionRevision: 3, TargetUUID: second.UUID}, 1)
	require.ErrorIs(t, err, models.ErrEnrichmentInvalid)
	// The same URL from two old definitions cannot produce two new targets.
	require.ErrorIs(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.EnrichmentWork.PreviewRebind(ctx, enrichmentRebindInput(collection, first, second))
		return err
	}), models.ErrEnrichmentConflict)
	plan := previewEnrichmentRebind(t, repo, enrichmentRebindInput(collection, first))
	_, err = applyEnrichmentRebind(repo, plan, now)
	require.NoError(t, err)
	rows, err = read(3, nil, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "replacement_exists", rows[0].Disposition)
	require.ErrorIs(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.EnrichmentWork.PreviewRebind(ctx, enrichmentRebindInput(collection, second))
		return err
	}), models.ErrEnrichmentConflict)
	definition := collection.SourceCollectionDefinition
	definition.State = "disabled"
	collection, err = reviseEnrichmentCollection(repo, collection.UUID, 3, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: definition})
	require.NoError(t, err)
	rows, err = read(4, nil, 100)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, "collection_disabled", row.Disposition)
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	query, err := raw.Query(`EXPLAIN QUERY PLAN SELECT uuid FROM enrichment_targets INDEXED BY enrichment_targets_pending_scope
WHERE collection_uuid=? AND state='pending' AND collection_revision<? AND (collection_revision,uuid)>(?,?) ORDER BY collection_revision,uuid LIMIT 100`, collection.UUID, 4, 0, "")
	require.NoError(t, err)
	defer query.Close()
	var descriptions []string
	for query.Next() {
		var a, b, c int
		var description string
		require.NoError(t, query.Scan(&a, &b, &c, &description))
		descriptions = append(descriptions, description)
	}
	require.NoError(t, query.Err())
	require.Contains(t, strings.Join(descriptions, "\n"), "SEARCH enrichment_targets USING")
	require.NotContains(t, strings.Join(descriptions, "\n"), "TEMP B-TREE")
}

func TestEnrichmentRebindLeavesAttemptedWorkAndCheckpointsForRecovery(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
	require.NoError(t, err)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		queued, err := f.repo.ArchiveJob.Finish(ctx, running.Lease(), f.now, models.ArchiveJobOutcome{State: "retry", ErrorCode: "rate_limited", RetryAt: f.now.Add(time.Hour), Result: []byte(`{}`)})
		if err != nil {
			return err
		}
		_, err = f.repo.ArchiveJob.Cancel(ctx, queued.UUID, queued.Revision, f.now)
		return err
	}))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		f.target, err = f.repo.EnrichmentWork.Retry(ctx, f.target.UUID, f.target.Revision, f.now)
		return err
	}))
	f.collection = changedEnrichmentCollection(t, f.repo, f.collection)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var currentBinding bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM enrichment_job_targets WHERE target_uuid=? AND target_revision=?)", f.target.UUID, f.target.Revision).Scan(&currentBinding))
	require.False(t, currentBinding)
	before := map[string][][]any{}
	for _, table := range []string{"enrichment_targets", "enrichment_target_history", "enrichment_job_targets", "enrichment_checkpoint_receipts", "archive_jobs"} {
		before[table] = albumJobRows(t, raw, table)
	}
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		candidates, err := f.repo.EnrichmentWork.RebindCandidates(ctx, f.collection.UUID, f.collection.Revision, nil, 100)
		require.NoError(t, err)
		require.Len(t, candidates, 1)
		require.Equal(t, "worker_history", candidates[0].Disposition)
		_, err = f.repo.EnrichmentWork.PreviewRebind(ctx, enrichmentRebindInput(f.collection, f.target))
		require.ErrorIs(t, err, models.ErrEnrichmentConflict)
		return nil
	}))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_rebindings"))
}

func TestEnrichmentRebindRejectsChangedTargetAndForgottenSource(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	input, collection := enrichmentInput(t, repo)
	now := time.Now().UTC()
	target := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending"}, now)
	require.ErrorIs(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.EnrichmentWork.PreviewRebind(ctx, enrichmentRebindInput(collection, target))
		return err
	}), models.ErrEnrichmentConflict, "unchanged collection scope does not need a new target")
	collection = changedEnrichmentCollection(t, repo, collection)
	plan := previewEnrichmentRebind(t, repo, enrichmentRebindInput(collection, target))
	held, err := scheduleEnrichment(repo, target, models.EnrichmentSchedule{State: "held", Priority: 40}, now.Add(time.Minute))
	require.NoError(t, err)
	_, err = applyEnrichmentRebind(repo, plan, now.Add(time.Minute))
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	require.Equal(t, held, readEnrichmentTarget(t, repo, target.UUID))
	// A second URL gives independent pending work in the current scope.
	url, err := observePostURL(repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(input.PostUUID), URL: "https://twitter.com/source/status/123"})
	require.NoError(t, err)
	input.URLUUID, input.CollectionRevision = url.URLUUID, collection.Revision
	pending := retainEnrichment(t, repo, input, models.EnrichmentSchedule{State: "pending"}, now)
	collection = changedEnrichmentCollection(t, repo, collection)
	plan = previewEnrichmentRebind(t, repo, enrichmentRebindInput(collection, pending))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten',revision=revision+1 WHERE uuid=?", input.PostUUID)
	require.NoError(t, err)
	_, err = applyEnrichmentRebind(repo, plan, now.Add(2*time.Minute))
	require.ErrorIs(t, err, models.ErrEnrichmentConflict)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := repo.EnrichmentWork.RebindCandidates(ctx, collection.UUID, collection.Revision, nil, 100)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "post_forgotten", rows[0].Disposition)
		return nil
	}))
	require.Equal(t, pending, readEnrichmentTarget(t, repo, pending.UUID))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_activations"))
}
