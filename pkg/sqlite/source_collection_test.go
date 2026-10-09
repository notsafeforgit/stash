package sqlite_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func putMediaRoot(t *testing.T, repo models.Repository, input models.MediaRootInput) *models.MediaRoot {
	t.Helper()
	var ret *models.MediaRoot
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { var err error; ret, err = repo.MediaRoot.Put(ctx, input); return err }))
	return ret
}

func putSourceCollection(t *testing.T, repo models.Repository, input models.SourceCollectionInput) *models.SourceCollection {
	t.Helper()
	var ret *models.SourceCollection
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceCollection.Put(ctx, input)
		return err
	}))
	return ret
}

func TestMediaRootRevisionAndMountReview(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	mount := filepath.Join(t.TempDir(), "media")
	require.NoError(t, os.Mkdir(mount, 0700))
	binding, err := archive.ProbeMediaRoot(mount)
	require.NoError(t, err)
	id := uuid.NewString()
	input := models.MediaRootInput{UUID: strings.ToUpper(id), Origin: "review", Reason: "Initial mount",
		MediaRootDefinition: models.MediaRootDefinition{Label: "Library", State: "active", Binding: binding}}
	first := putMediaRoot(t, repo, input)
	require.Equal(t, id, first.UUID)
	require.Equal(t, 1, first.Revision)
	stale := repo.WithTxn(context.Background(), func(ctx context.Context) error { _, err := repo.MediaRoot.Put(ctx, input); return err })
	require.ErrorIs(t, stale, models.ErrSourceDefinitionConflict)
	input.UUID, input.ExpectedRevision = id, 1
	require.Equal(t, first, putMediaRoot(t, repo, input), "identical definitions do not create another revision")
	require.NoError(t, os.Rename(mount, mount+"-old"))
	require.NoError(t, os.Mkdir(mount, 0700))
	input.State = "disabled"
	disabled := putMediaRoot(t, repo, input)
	require.Equal(t, 2, disabled.Revision)
	input.ExpectedRevision, input.State = 2, "active"
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error { _, err := repo.MediaRoot.Put(ctx, input); return err })
	require.ErrorContains(t, err, "directory changed")
	input.Binding, err = archive.ProbeMediaRoot(mount)
	require.NoError(t, err)
	rebound := putMediaRoot(t, repo, input)
	require.Equal(t, id, rebound.UUID)
	require.NotEqual(t, first.Binding.DirectoryIdentity, rebound.Binding.DirectoryIdentity)
	input.ExpectedRevision, input.State = 3, "retired"
	retired := putMediaRoot(t, repo, input)
	input.ExpectedRevision, input.State = 4, "active"
	require.NoError(t, os.Rename(mount, mount+"-retired"))
	require.NoError(t, os.Mkdir(mount, 0700))
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error { _, err := repo.MediaRoot.Put(ctx, input); return err })
	require.ErrorContains(t, err, "directory changed", "restoring a retired root must recheck its binding")
	input.Binding, err = archive.ProbeMediaRoot(mount)
	require.NoError(t, err)
	restored := putMediaRoot(t, repo, input)
	require.Equal(t, retired.UUID, restored.UUID)
	require.Equal(t, 5, restored.Revision)
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error { _, err := repo.MediaRoot.Put(ctx, input); return err })
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict, "restoration does not bypass revision checks")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		current, err := repo.MediaRoot.Find(ctx, id)
		require.NoError(t, err)
		require.Equal(t, restored, current)
		history, err := repo.MediaRoot.History(ctx, id, 0, 2)
		require.NoError(t, err)
		require.Len(t, history, 2)
		require.Equal(t, *first, history[0].MediaRoot)
		require.Equal(t, "Initial mount", history[0].Reason)
		require.False(t, history[0].RecordedAt.IsZero())
		next, err := repo.MediaRoot.History(ctx, id, history[1].Revision, 2)
		require.NoError(t, err)
		require.Len(t, next, 2)
		require.Equal(t, *rebound, next[0].MediaRoot)
		require.Equal(t, *retired, next[1].MediaRoot)
		return nil
	}))
}

func TestSourceCollectionsKeepTargetsIndependentFromAttribution(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "migration", MediaRootDefinition: models.MediaRootDefinition{Label: "Imported root", State: "active"}})
	account := createSourceAccount(t, repo, "native:reddit")
	input := models.SourceCollectionInput{UUID: uuid.NewString(), Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Aggregator submissions", Kind: "account", State: "active", Namespace: account.Namespace, AccountUUID: &account.UUID,
		TargetURL: "https://www.reddit.com/user/aggregator/saved/", RootUUID: &root.UUID, PathPrefix: "Aggregator, reddit"}}
	first := putSourceCollection(t, repo, input)
	input.ExpectedRevision = first.Revision
	input.TargetURL = "https://www.reddit.com/user/aggregator/upvoted/"
	second := putSourceCollection(t, repo, input)
	duplicate := input
	duplicate.UUID, duplicate.ExpectedRevision, duplicate.Label = uuid.NewString(), 0, "Separate scrape policy"
	other := putSourceCollection(t, repo, duplicate)
	manual := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Purchased MP4 batch", Kind: "manual_batch", State: "active", RootUUID: &root.UUID, PathPrefix: "Purchased"}})
	require.Nil(t, manual.AccountUUID)
	require.Empty(t, manual.Namespace)
	require.Empty(t, manual.TargetURL)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		matches, err := repo.SourceCollection.LookupTarget(ctx, first.TargetURL, "", 100)
		require.NoError(t, err)
		require.Equal(t, []*models.SourceCollection{second}, matches)
		matches, err = repo.SourceCollection.LookupTarget(ctx, second.TargetURL, "", 1)
		require.NoError(t, err)
		require.Len(t, matches, 1)
		next, err := repo.SourceCollection.LookupTarget(ctx, second.TargetURL, matches[0].UUID, 1)
		require.NoError(t, err)
		require.Len(t, next, 1)
		require.ElementsMatch(t, []string{second.UUID, other.UUID}, []string{matches[0].UUID, next[0].UUID})
		history, err := repo.SourceCollection.History(ctx, first.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 2)
		require.Equal(t, *first, history[0].SourceCollection)
		all, err := repo.SourceCollection.List(ctx, "", 100)
		require.NoError(t, err)
		require.Len(t, all, 3)
		r, err := repo.MediaRoot.List(ctx, "", 100)
		require.NoError(t, err)
		require.Len(t, r, 1)
		ownership, err := repo.SourceAccount.Ownership(ctx, account.UUID)
		require.NoError(t, err)
		require.Nil(t, ownership)
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_accounts"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_posts"))
	rows, err := raw.Query(`EXPLAIN QUERY PLAN SELECT collection_uuid FROM source_collection_revisions
WHERE target_url=? AND target_url!='' AND collection_uuid>? GROUP BY collection_uuid ORDER BY collection_uuid LIMIT 10`, first.TargetURL, "")
	require.NoError(t, err)
	defer rows.Close()
	var plan string
	for rows.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, rows.Scan(&a, &b, &c, &detail))
		plan += detail + "\n"
	}
	require.NoError(t, rows.Err())
	require.Contains(t, plan, "source_collection_target")
	require.NotContains(t, plan, "SCAN source_collection_revisions")
	// The account relation is service-qualified, including at the SQL boundary.
	bad := input
	bad.UUID, bad.ExpectedRevision, bad.Namespace = uuid.NewString(), 0, "native:twitter"
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceCollection.Put(ctx, bad)
		require.ErrorIs(t, err, models.ErrSourceDefinitionInvalid)
		return nil
	})
	require.NoError(t, err, "reference validation happens before any identity is inserted")
	require.Equal(t, uint(3), queryUint(t, raw, "SELECT count(*) FROM source_collections"))
	// The SQL boundary must still reject a wrong service and prevent a caller
	// that ignores that failure from committing an orphan identity.
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, "INSERT INTO source_collections(uuid) VALUES(?)", []interface{}{bad.UUID})
		require.NoError(t, err)
		_, _, err = db.ExecSQL(ctx, `INSERT INTO source_collection_revisions
(collection_uuid,revision,label,kind,namespace,state,target_url,account_uuid,path_prefix,origin,reason)
VALUES(?,1,'Invalid service','account','native:twitter','active','',?,'','review','')`, []interface{}{bad.UUID, account.UUID})
		require.Error(t, err)
		return nil
	})
	require.Error(t, err, "SQL constraints prevent an orphan identity when the failed insert is ignored")
	require.Equal(t, uint(3), queryUint(t, raw, "SELECT count(*) FROM source_collections"))
}

func TestCollectionCaptureAndManualIntakeReplay(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	input := models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Manual batch", Kind: "manual_batch", State: "active"}}
	first := putSourceCollection(t, repo, input)
	input.UUID, input.ExpectedRevision, input.Label = first.UUID, 1, "Renamed batch"
	second := putSourceCollection(t, repo, input)
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	intake := models.CollectionMediaIntake{UUID: uuid.NewString(), CollectionUUID: first.UUID, CollectionRevision: 1, MediaUUID: scene.UUID, Origin: "scan", Reason: "Direct import"}
	var recorded *models.CollectionMediaIntake
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		recorded, err = repo.SourceCollection.RecordMediaIntake(ctx, intake)
		require.NoError(t, err)
		replay, err := repo.SourceCollection.RecordMediaIntake(ctx, intake)
		require.NoError(t, err)
		require.Equal(t, recorded, replay)
		return nil
	}))
	for _, mutate := range []func(*models.CollectionMediaIntake){func(i *models.CollectionMediaIntake) { i.Reason = "Different" }, func(i *models.CollectionMediaIntake) { i.CollectionRevision = 2 }} {
		bad := intake
		mutate(&bad)
		err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
			_, err := repo.SourceCollection.RecordMediaIntake(ctx, bad)
			return err
		})
		require.ErrorIs(t, err, models.ErrCollectionIntakeReplay)
	}
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "album"}, "")
	capture := recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 1, "Bio"))
	for _, revision := range []int{1, 2, 1} {
		require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
			return repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: first.UUID, CollectionRevision: revision, CaptureUUID: capture.UUID})
		}))
	}
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		for _, revision := range []int{1, 2, 3} {
			present, err := repo.SourceCollection.HasCapture(ctx, models.CollectionCapture{CollectionUUID: first.UUID, CollectionRevision: revision, CaptureUUID: capture.UUID})
			require.NoError(t, err)
			require.Equal(t, revision <= 2, present)
		}
		present, err := repo.SourceCollection.HasCapture(ctx, models.CollectionCapture{CollectionUUID: uuid.NewString(), CollectionRevision: 1, CaptureUUID: capture.UUID})
		require.NoError(t, err)
		require.False(t, present)
		page, err := repo.SourceCollection.Captures(ctx, first.UUID, nil, 1)
		require.NoError(t, err)
		require.Len(t, page, 1)
		require.Equal(t, 1, page[0].CollectionRevision)
		next, err := repo.SourceCollection.Captures(ctx, first.UUID, &models.CollectionCaptureCursor{CaptureUUID: page[0].CaptureUUID, CollectionRevision: page[0].CollectionRevision}, 1)
		require.NoError(t, err)
		require.Len(t, next, 1)
		require.Equal(t, 2, next[0].CollectionRevision)
		items, err := repo.SourceCollection.MediaIntake(ctx, first.UUID, "", 100)
		require.NoError(t, err)
		require.Equal(t, []models.CollectionMediaIntake{*recorded}, items)
		return nil
	}))
	input.ExpectedRevision, input.State = second.Revision, "retired"
	retired := putSourceCollection(t, repo, input)
	// Retired definitions can still receive previously unimported provenance.
	intake.UUID = uuid.NewString()
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceCollection.RecordMediaIntake(ctx, intake)
		return err
	}))
	for _, state := range []string{"disabled", "active", "retired", "active"} {
		input.ExpectedRevision, input.State = retired.Revision, state
		retired = putSourceCollection(t, repo, input)
		require.Equal(t, first.UUID, retired.UUID)
		require.Equal(t, state, retired.State)
	}
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error { _, err := repo.SourceCollection.Put(ctx, input); return err })
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		items, err := repo.SourceCollection.MediaIntake(ctx, first.UUID, "", 100)
		require.NoError(t, err)
		require.Len(t, items, 2, "restoration preserves previously recorded intake")
		history, err := repo.SourceCollection.History(ctx, first.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 7)
		require.Equal(t, "retired", history[2].State)
		require.Equal(t, "active", history[6].State)
		return nil
	}))
	performer := archiveFind(t, repo, models.ArchivePerformer, 71)
	intake.UUID, intake.MediaUUID = uuid.NewString(), performer.UUID
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceCollection.RecordMediaIntake(ctx, intake)
		return err
	})
	require.ErrorContains(t, err, "scene or image")
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, query := range []string{"UPDATE source_collection_captures SET collection_revision=2", "UPDATE source_collection_media_intake SET reason='rewrite'", "UPDATE source_collection_revisions SET label='rewrite'", "UPDATE source_collections SET revision=1"} {
		_, err := raw.Exec(query)
		require.Error(t, err, query)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceCollectionMigrationAndAnonymisation(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-collections.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+14; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	defer raw.Close()
	// Raw historical setup omits relationship edits that require the current
	// repository's pre-commit decision finalizer.
	_, err = raw.Exec(strings.ReplaceAll(archiveIdentityFixture, "INSERT INTO performers_scenes(performer_id, scene_id) VALUES (71, 31);", ""))
	require.NoError(t, err)
	account := uuid.NewString()
	_, err = raw.Exec("INSERT INTO source_accounts(uuid,namespace,label) VALUES(?,'native:reddit','Retained account')", account)
	require.NoError(t, err)
	var sceneUUID string
	require.NoError(t, raw.QueryRow("SELECT uuid FROM archive_entities WHERE scene_id=31").Scan(&sceneUUID))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	repo := db.Repository()
	require.Equal(t, sceneUUID, archiveFind(t, repo, models.ArchiveScene, 31).UUID)
	require.Equal(t, "Retained account", findSourceAccount(t, repo, account).Label)
	for _, table := range []string{"media_roots", "source_collections", "source_collection_captures", "source_collection_media_intake"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), "migration must not fabricate provenance")
	}
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Private mount", State: "disabled"}})
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Private feed", Kind: "feed", State: "active", Namespace: "native:reddit", AccountUUID: &account, RootUUID: &root.UUID, PathPrefix: "Private folder", TargetURL: "https://example.test/private-feed"}})
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceCollection.RecordMediaIntake(ctx, models.CollectionMediaIntake{UUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: 1, MediaUUID: sceneUUID, Origin: "migration", Reason: "Private provenance"})
		return err
	}))
	out := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anon, err := sqlite.NewAnonymiser(db, out)
	require.NoError(t, err)
	require.NoError(t, anon.Anonymise(context.Background()))
	anonymous := openRawDB(t, out)
	defer anonymous.Close()
	for _, table := range []string{"media_roots", "media_root_revisions", "source_collections", "source_collection_revisions", "source_collection_captures", "source_collection_media_intake", "source_accounts"} {
		require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceDefinitionValidationAndSQLPublicationGuards(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	ctx := context.Background()
	for _, bad := range []models.SourceCollectionInput{
		{SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Example", Kind: "manual_batch", State: "active", PathPrefix: "unbound"}, Origin: "review"},
		{SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Example", Kind: "invented", State: "active"}, Origin: "review"},
		{SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Example", Kind: "feed", State: "active", TargetURL: "file:///etc/passwd"}, Origin: "review"},
		{SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Example", Kind: "feed", State: "active", TargetURL: "https://user:secret@example.test/"}, Origin: "review"},
		{SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Example", Kind: "feed", State: "active", Namespace: "reddit"}, Origin: "review"},
	} {
		err := repo.WithTxn(ctx, func(ctx context.Context) error { _, err := repo.SourceCollection.Put(ctx, bad); return err })
		require.Error(t, err)
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, table := range []string{"media_roots", "source_collections"} {
		_, err := raw.Exec("INSERT INTO "+table+"(uuid) VALUES(?)", uuid.NewString())
		require.ErrorContains(t, err, "FOREIGN KEY", "standalone base insertion must not publish an incomplete definition")
		_, err = raw.Exec("INSERT INTO " + table + "(uuid) VALUES('invalid')")
		require.ErrorContains(t, err, "CHECK")
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Root", State: "active"}})
	_, err := raw.Exec("UPDATE media_root_revisions SET label='rewrite'")
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("UPDATE media_roots SET revision=2")
	require.ErrorContains(t, err, "recorded revision")
	_, err = raw.Exec(`INSERT INTO media_root_revisions(root_uuid,revision,label,state,origin,reason) VALUES(?,3,'Skipped','active','review','')`, root.UUID)
	require.ErrorContains(t, err, "stale")
	require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
		_, err := repo.MediaRoot.List(ctx, "", 101)
		require.Error(t, err)
		_, err = repo.SourceCollection.List(ctx, "invalid", 10)
		require.Error(t, err)
		_, err = repo.SourceCollection.Captures(ctx, uuid.NewString(), &models.CollectionCaptureCursor{CaptureUUID: uuid.NewString()}, 1)
		require.Error(t, err)
		return nil
	}))
}

func TestCollectionIntakeSurvivesUUIDAdoptionAndDeletion(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Purchased batch", Kind: "manual_batch", State: "active"}})
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	input := models.CollectionMediaIntake{UUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: 1, MediaUUID: scene.UUID, Origin: "scan"}
	adopted := uuid.NewString()
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceCollection.RecordMediaIntake(ctx, input)
		require.NoError(t, err)
		_, err = repo.ArchiveEntity.AdoptUUID(ctx, scene.UUID, adopted, scene.Revision)
		return err
	}))
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		replay, err := repo.SourceCollection.RecordMediaIntake(ctx, input)
		require.NoError(t, err)
		require.Equal(t, adopted, replay.MediaUUID)
		require.Equal(t, scene.UUID, replay.SubmittedMediaUUID)
		_, _, err = db.ExecSQL(ctx, "DELETE FROM scenes WHERE id=31", nil)
		return err
	}))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		items, err := repo.SourceCollection.MediaIntake(ctx, collection.UUID, "", 1)
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Equal(t, adopted, items[0].MediaUUID)
		entity, err := repo.ArchiveEntity.Find(ctx, adopted)
		require.NoError(t, err)
		require.Equal(t, models.ArchiveEntityDeleted, entity.State)
		end, err := repo.SourceCollection.MediaIntake(ctx, collection.UUID, items[0].UUID, 1)
		require.NoError(t, err)
		require.Empty(t, end)
		return nil
	}))
}
