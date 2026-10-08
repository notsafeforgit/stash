package identify_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stashapp/stash/internal/identify"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
	"github.com/stashapp/stash/pkg/stashbox"
	"github.com/stretchr/testify/require"
)

type providerHookCounter struct{ calls int }

type providerFixtureScraper struct{ result *models.ScrapedScene }

func (s providerFixtureScraper) ScrapeScenes(context.Context, int) ([]*models.ScrapedScene, error) {
	return []*models.ScrapedScene{s.result}, nil
}

func (h *providerHookCounter) ExecuteSceneUpdatePostHooks(context.Context, models.SceneUpdateInput, []string) {
	h.calls++
}

func providerIdentificationFixture(t *testing.T) (*sqlite.Database, models.Repository, *identify.SceneIdentifier, *models.ScrapedScene, *providerHookCounter) {
	t.Helper()
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	db.SetBlobStoreOptions(sqlite.BlobStoreOptions{UseDatabase: true})
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "provider-identification.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO scenes(id,title,details,created_at,updated_at) VALUES(1,'Local title','Local details',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, nil)
		return err
	}))
	title, details, name := "Provider title", "Ignored provider details", "Provider Performer"
	sceneRemote, performerRemote, studioRemote, parentRemote, tagRemote := "scene-remote", "performer-remote", "studio-remote", "parent-remote", "tag-remote"
	source := &models.ScrapedScene{Title: &title, Details: &details, RemoteSiteID: &sceneRemote,
		Performers: []*models.ScrapedPerformer{{Name: &name, RemoteSiteID: &performerRemote}},
		Studio: &models.ScrapedStudio{Name: "Child studio", RemoteSiteID: &studioRemote,
			Parent: &models.ScrapedStudio{Name: "Parent studio", RemoteSiteID: &parentRemote}},
		Tags: []*models.ScrapedTag{{Name: "Provider tag", RemoteSiteID: &tagRemote}}}
	source.Studio.Images = []string{"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg=="}
	yes, no := true, false
	hooks := &providerHookCounter{}
	task := &identify.SceneIdentifier{TxnManager: repo.TxnManager, SceneReaderUpdater: repo.Scene,
		StudioReaderWriter: repo.Studio, PerformerCreator: repo.Performer, TagFinderCreator: repo.Tag,
		SceneUpdatePostHookExecutor: hooks, RecordProviderMetadata: stashbox.NewMetadataRecorder(repo, "identify"),
		DefaultOptions: &identify.MetadataOptions{SetCoverImage: &no, SetOrganized: &yes,
			FieldOptions: []*identify.FieldOptions{{Field: "title", Strategy: identify.FieldStrategyOverwrite}, {Field: "details", Strategy: identify.FieldStrategyIgnore},
				{Field: "studio", Strategy: identify.FieldStrategyMerge, CreateMissing: &yes},
				{Field: "performers", Strategy: identify.FieldStrategyMerge, CreateMissing: &yes},
				{Field: "tags", Strategy: identify.FieldStrategyMerge, CreateMissing: &yes}}},
		Sources: []identify.ScraperSource{{Name: "Fixture provider", RemoteSite: "https://provider.invalid/graphql",
			Scraper: providerFixtureScraper{source}}}}
	return db, repo, task, source, hooks
}

func runProviderIdentification(t *testing.T, repo models.Repository, task *identify.SceneIdentifier) error {
	t.Helper()
	var scene *models.Scene
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		scene, err = repo.Scene.Find(ctx, 1)
		return err
	}))
	return task.Identify(t.Context(), scene)
}

func TestProviderIdentificationRecordsSceneAndCreatedEntities(t *testing.T) {
	db, repo, task, source, hooks := providerIdentificationFixture(t)
	require.NoError(t, runProviderIdentification(t, repo, task))
	require.Equal(t, 1, hooks.calls)
	require.Nil(t, source.Studio.Parent.StoredID, "a remote result must not acquire transient local IDs")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		scene, err := repo.Scene.Find(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, "Provider title", scene.Title)
		require.Equal(t, "Local details", scene.Details)
		require.True(t, scene.Organized)
		for kind, localIDs := range map[models.ArchiveEntityKind][]int{models.ArchiveScene: {1}, models.ArchiveStudio: {1, 2}, models.ArchivePerformer: {1}, models.ArchiveTag: {1}} {
			for _, id := range localIDs {
				entity, err := repo.ArchiveEntity.FindByLocalID(ctx, kind, id)
				require.NoError(t, err)
				require.NotNil(t, entity)
				history, err := repo.ProviderMetadata.History(ctx, entity.UUID, 0, 100)
				require.NoError(t, err)
				require.Len(t, history, 1, "%s %d", kind, id)
				require.Equal(t, "identify", history[0].Operation)
				require.Equal(t, task.Sources[0].RemoteSite, history[0].Endpoint)
				if kind == models.ArchiveScene {
					var accepted map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(history[0].Values, &accepted))
					require.Contains(t, accepted, "title")
					require.Contains(t, accepted, "studio")
					require.Contains(t, accepted, "performers")
					require.Contains(t, accepted, "tags")
					require.NotContains(t, accepted, "details")
					require.NotContains(t, accepted, "organized")
					require.NotContains(t, accepted, "stash_ids")
				}
				if kind == models.ArchiveStudio && id == 2 {
					var accepted map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(history[0].Values, &accepted))
					require.Contains(t, accepted, "image")
					require.NotContains(t, string(accepted["image"]), "base64")
				}
			}
		}
		return nil
	}))
}

func TestProviderIdentificationRollsBackAllRelatedImportsAndCanRetry(t *testing.T) {
	_, repo, task, source, hooks := providerIdentificationFixture(t)
	remote := source.RemoteSiteID
	source.RemoteSiteID = nil // fail the final receipt after related imports succeeded
	require.ErrorContains(t, runProviderIdentification(t, repo, task), "provider metadata")
	require.Zero(t, hooks.calls)
	require.Nil(t, source.Studio.Parent.StoredID)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		scene, err := repo.Scene.Find(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, "Local title", scene.Title)
		for _, kind := range []models.ArchiveEntityKind{models.ArchiveStudio, models.ArchivePerformer, models.ArchiveTag} {
			entity, err := repo.ArchiveEntity.FindByLocalID(ctx, kind, 1)
			require.NoError(t, err)
			require.Nil(t, entity)
		}
		entity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveScene, 1)
		require.NoError(t, err)
		history, err := repo.ProviderMetadata.History(ctx, entity.UUID, 0, 100)
		require.NoError(t, err)
		require.Empty(t, history)
		return nil
	}))
	source.RemoteSiteID = remote
	require.NoError(t, runProviderIdentification(t, repo, task))
	require.Equal(t, 1, hooks.calls)
}

func TestProviderIdentificationRequiresRecorderButOrdinaryScrapersDoNot(t *testing.T) {
	_, repo, task, _, hooks := providerIdentificationFixture(t)
	task.RecordProviderMetadata = nil
	require.ErrorContains(t, runProviderIdentification(t, repo, task), "recorder is required")
	require.Zero(t, hooks.calls)
	task.Sources[0].RemoteSite = ""
	require.NoError(t, runProviderIdentification(t, repo, task))
	require.Equal(t, 1, hooks.calls)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		entity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveScene, 1)
		require.NoError(t, err)
		history, err := repo.ProviderMetadata.History(ctx, entity.UUID, 0, 100)
		require.NoError(t, err)
		require.Empty(t, history)
		return nil
	}))
}

func TestProviderIdentificationPreservesMatchedParentMetadata(t *testing.T) {
	_, repo, task, source, _ := providerIdentificationFixture(t)
	parent := models.NewCreateStudioInput()
	parent.Name, parent.Details = "Curated parent", "Curated details"
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Studio.Create(ctx, &parent) }))
	id := strconv.Itoa(parent.ID)
	source.Studio.Parent.StoredID = &id
	require.NoError(t, runProviderIdentification(t, repo, task))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.Studio.Find(ctx, parent.ID)
		require.NoError(t, err)
		require.Equal(t, parent.Name, current.Name)
		require.Equal(t, parent.Details, current.Details)
		entity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveStudio, parent.ID)
		require.NoError(t, err)
		history, err := repo.ProviderMetadata.History(ctx, entity.UUID, 0, 100)
		require.NoError(t, err)
		require.Empty(t, history)
		return nil
	}))
}
