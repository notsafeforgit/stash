package sqlite_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func removeSourceSubscriptionSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeLibraryUUIDSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE source_run_subscriptions; DROP TABLE source_subscriptions;
DELETE FROM native_migration_history WHERE version=1000114`)
	require.NoError(t, err)
}

func TestSourceSubscriptionMigrationRetainsRunningDownloadsAndDefaultsOff(t *testing.T) {
	f := newSourceRunFixture(t)
	running := f.claim(t, f.submit(t, f.request()))
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	removeSourceSubscriptionSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000113")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.Error(t, f.db.Open(path))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	require.Equal(t, running, f.find(t, running.UUID))
	page, err := f.service.Subscriptions(t.Context(), f.token, f.root.UUID, f.collection.Namespace, "")
	require.NoError(t, err)
	require.Empty(t, page.Items)
}

func setSubscription(t *testing.T, repo models.Repository, id string, revision int, enabled bool) *models.SourceSubscription {
	t.Helper()
	var result *models.SourceSubscription
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = repo.SourceSubscription.Put(ctx, id, models.SourceSubscriptionInput{ExpectedRevision: revision, Enabled: enabled})
		return err
	}))
	return result
}

func TestSourceSubscriptionsCancelQueuedButPreserveRunningAndManualWork(t *testing.T) {
	f := newSourceRunFixture(t)
	page, err := f.service.Subscriptions(t.Context(), f.token, f.root.UUID, f.collection.Namespace, "")
	require.NoError(t, err)
	require.Empty(t, page.Items, "new sources default to monitoring off")
	setSubscription(t, f.repo, f.collection.UUID, 0, true)
	input := f.request()
	input.SubscriptionRevision = 1
	run := f.claim(t, f.submit(t, input))
	require.NotNil(t, run)
	queuedInput := input
	queuedInput.RequestUUID = uuid.NewString()
	queuedInput.PolicySHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	queued := f.submit(t, queuedInput)
	manual := f.submit(t, f.request())
	require.NotEqual(t, run.UUID, manual.UUID)
	setSubscription(t, f.repo, f.collection.UUID, 1, false)
	require.Equal(t, "cancelled", f.find(t, queued.UUID).State)
	require.Equal(t, "queued", f.find(t, manual.UUID).State)
	require.Equal(t, "running", f.find(t, run.UUID).State)
	require.Equal(t, "succeeded", f.finish(t, run, "succeeded").State)
	input.RequestUUID = uuid.NewString()
	late := f.submit(t, input)
	require.Equal(t, "cancelled", late.State, "offline snapshots cannot restart monitoring")
	require.Equal(t, late, f.submit(t, input), "lost cancellation responses replay normally")
	setSubscription(t, f.repo, f.collection.UUID, 2, true)
	input.RequestUUID = uuid.NewString()
	require.Equal(t, "cancelled", f.submit(t, input).State, "reenabling does not revive stale snapshots")
	input.RequestUUID = uuid.NewString()
	input.SubscriptionRevision = 3
	require.Equal(t, "queued", f.submit(t, input).State)
	page, err = f.service.Subscriptions(t.Context(), f.token, f.root.UUID, f.collection.Namespace, "")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, 3, page.Items[0].Revision)
	_, err = f.service.Subscriptions(t.Context(), f.token, uuid.NewString(), f.collection.Namespace, "")
	require.ErrorIs(t, err, ingest.ErrForbidden)
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceSubscription.Put(ctx, f.collection.UUID, models.SourceSubscriptionInput{ExpectedRevision: 1, Enabled: false})
		return err
	}), models.ErrSourceDefinitionConflict)
}

func TestSourceSubscriptionsStopExactPerformerOwnershipThroughMerges(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, "INSERT INTO performers(id,created_at,updated_at) VALUES(73,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)")
	attachmentSQL(t, db, "INSERT INTO performer_names(performer_id,name,position) VALUES(73,'Another performer',0)")
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Library", State: "active"}})
	var collections []*models.SourceCollection
	for _, owner := range []int{71, 72, 73, 0} {
		account := createSourceAccount(t, repo, "native:reddit")
		if owner != 0 {
			_, _, err := applyAccountReview(repo, accountReviewRequest(t, repo, accountReviewInput(t, repo, account.UUID, owner)))
			require.NoError(t, err)
		}
		collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Profile", Kind: "account", Namespace: "native:reddit", State: "active", TargetURL: "https://example.org/" + account.UUID,
			RootUUID: &root.UUID, PathPrefix: ".", AccountUUID: &account.UUID}})
		collections = append(collections, collection)
		setSubscription(t, repo, collection.UUID, 0, true)
	}
	old := archiveFind(t, repo, models.ArchivePerformer, 71)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		page, err := repo.SourceSubscription.List(ctx, models.SourceSubscriptionFilter{PerformerUUID: old.UUID, Limit: 1})
		require.NoError(t, err)
		require.Equal(t, 2, page.Total)
		require.Len(t, page.Items, 1)
		return nil
	}))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		count, err := repo.SourceSubscription.StopPerformer(ctx, old.UUID)
		require.Equal(t, 2, count)
		return err
	}))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for i, c := range collections {
			s, err := repo.SourceSubscription.Find(ctx, c.UUID)
			require.NoError(t, err)
			require.Equal(t, i >= 2, s.Enabled, "unowned and other performers' accounts are not affected")
		}
		return nil
	}))
}

func TestSourceSubscriptionAdoptsExistingSchedulerRunWithoutChangingProgress(t *testing.T) {
	f := newSourceRunFixture(t)
	run := f.submit(t, f.request())
	setSubscription(t, f.repo, f.collection.UUID, 0, true)
	bind := func(revision int64) error {
		return f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			return f.repo.SourceSubscription.BindRun(ctx, run.UUID, models.SourceRunSubscriptionInput{
				ExpectedRunRevision: revision, SubscriptionRevision: 1})
		})
	}
	require.ErrorIs(t, bind(run.Revision+1), models.ErrSourceDefinitionConflict)
	require.NoError(t, bind(run.Revision))
	require.Equal(t, run, f.find(t, run.UUID), "adopting a queued run preserves its original admission and progress")
	setSubscription(t, f.repo, f.collection.UUID, 1, false)
	require.Equal(t, "cancelled", f.find(t, run.UUID).State)
	require.NoError(t, bind(run.Revision), "receipt replay does not resubscribe")
}
