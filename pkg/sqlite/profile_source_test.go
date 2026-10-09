package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeProfileSourceSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP TRIGGER source_collection_alias_frozen;
DROP TABLE source_collection_aliases;
DROP TABLE source_run_retrievals;
DELETE FROM native_migration_history WHERE version=1000106`)
	require.NoError(t, err)
}

func TestProfileSourcesMigrationPreservesPendingPassesAndPausesTogether(t *testing.T) {
	f := newSourceRunFixture(t)
	request := f.request()
	run := f.submit(t, request)
	urls, err := scrape.BackfillTargets(models.BackfillSubject{RootUUID: f.root.UUID, Platform: "reddit", Account: "example"}, "reddit-top")
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	removeProfileSourceSchema(t, raw)
	for _, target := range urls {
		id := uuid.NewString()
		tx, err := raw.Begin()
		require.NoError(t, err)
		_, err = tx.Exec("INSERT INTO source_collections(uuid) VALUES(?)", id)
		require.NoError(t, err)
		_, err = tx.Exec(`INSERT INTO source_collection_revisions(collection_uuid,revision,label,kind,namespace,state,target_url,root_uuid,path_prefix,origin,reason)
   VALUES(?,1,'Old retrieval','account','native:reddit','active',?,?,'Example','migration','Old fixture')`, id, target, f.root.UUID)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	}
	_, err = raw.Exec("UPDATE schema_migrations SET version=1000105")
	require.NoError(t, err)
	before := albumJobRows(t, raw, "source_run_requests")
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	require.Equal(t, before, albumJobRows(t, raw, "source_run_requests"))
	require.EqualValues(t, 5, queryUint(t, raw, "SELECT count(*) FROM source_collection_aliases"))
	var profile *models.SourceCollection
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.SourceCollection.Search(ctx, models.SourceDefinitionFilter{Limit: 100})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		profile = rows[0]
		require.Equal(t, "https://www.reddit.com/user/example/", profile.TargetURL)
		old, err := f.repo.SourceRun.List(ctx, profile.UUID, []*string{&f.root.UUID}, 0, 100)
		require.NoError(t, err)
		require.Len(t, old, 1)
		require.Equal(t, run.UUID, old[0].UUID)
		return nil
	}))
	replayed := f.submit(t, request)
	require.Equal(t, run, replayed, "the original request and execution URL survive migration")
	claimed := f.claim(t, run)
	require.NotNil(t, claimed)
	_, f.token, err = f.service.IssueCredential(t.Context(), f.producer.UUID, nil, nil, f.root.UUID)
	require.NoError(t, err)
	profileRequest := f.request()
	profileRequest.CollectionUUID, profileRequest.CollectionRevision = profile.UUID, profile.Revision
	profileRequest.RetrievalURL = urls[0]
	profileRun := f.submit(t, profileRequest)
	require.Nil(t, f.claim(t, profileRun), "new profile work cannot overlap its historical retrieval")
	f.finish(t, claimed, "retry")
	definition := profile.SourceCollectionDefinition
	definition.State = "disabled"
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: profile.UUID, ExpectedRevision: profile.Revision, Origin: "review", SourceCollectionDefinition: definition})
	f.now = f.now.Add(time.Hour)
	require.Nil(t, f.claim(t, f.find(t, run.UUID)), "pausing the profile also fences pending historical passes")
	require.Equal(t, "definition_changed", f.find(t, run.UUID).ErrorCode)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestProfileSourcesSeparatePassCoverageAndValidateRetrieval(t *testing.T) {
	f := newSourceRunFixture(t)
	definition := f.collection.SourceCollectionDefinition
	definition.Kind = "account"
	definition.TargetURL = "https://www.reddit.com/user/Example/"
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	request := f.request()
	request.RetrievalURL = "https://www.reddit.com/user/example/submitted/?sort=new"
	first := f.submit(t, request)
	require.Equal(t, request.RetrievalURL, first.TargetURL)
	require.Equal(t, first, f.submit(t, request))
	request.RequestUUID = uuid.NewString()
	request.RetrievalURL = "https://www.reddit.com/user/example/submitted/?sort=top&t=all"
	second := f.submit(t, request)
	require.NotEqual(t, first.UUID, second.UUID)
	require.NotNil(t, f.claim(t, first))
	require.Nil(t, f.claim(t, second), "only one profile pass may run at a time")
	for _, target := range []string{"https://www.reddit.com/user/other/submitted/?sort=new", "https://www.reddit.com/search?q=unrelated", "https://example.invalid/"} {
		request.RequestUUID = uuid.NewString()
		request.RetrievalURL = target
		_, err := f.coordinator.Submit(t.Context(), f.token, request)
		require.ErrorIs(t, err, models.ErrSourceRunInvalid)
	}
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: definition})
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
}
