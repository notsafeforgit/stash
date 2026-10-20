package identify_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/identify"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
	"github.com/stretchr/testify/require"
)

type fixedScraper struct{ results []*models.ScrapedScene }

func (s fixedScraper) ScrapeScenes(context.Context, int) ([]*models.ScrapedScene, error) {
	return s.results, nil
}

type quietHooks struct{}

func (quietHooks) ExecuteSceneUpdatePostHooks(context.Context, models.SceneUpdateInput, []string) {}

func TestIdentifyTagUUIDsResolveInsideWrite(t *testing.T) {
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "identify.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := entityid.WithTransactions(db.Repository())
	tag, survivor := models.NewTag(), models.NewTag()
	tag.Name, survivor.Name = "Skipped", "Merged skipped"
	scene := models.NewScene()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for _, tag := range []*models.Tag{&tag, &survivor} {
			if err := repo.Tag.Create(ctx, &models.CreateTagInput{Tag: tag}); err != nil {
				return err
			}
		}
		return repo.Scene.Create(ctx, &scene, nil)
	}))
	ctx := entityid.WithRepository(t.Context(), repo)
	tagUUID, err := entityid.Public(ctx, models.ArchiveTag, tag.ID)
	require.NoError(t, err)
	sceneUUID, err := entityid.Public(ctx, models.ArchiveScene, scene.ID)
	require.NoError(t, err)
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		return repo.Tag.Merge(ctx, []int{tag.ID}, survivor.ID)
	}))
	yes, no, name := true, false, "Mononym"
	for _, singleName := range []bool{false, true} {
		for _, reference := range []string{tagUUID, "1", sceneUUID} {
			options := &identify.MetadataOptions{
				SetCoverImage: &no, SkipMultipleMatches: &yes, SkipSingleNamePerformers: &yes,
				SkipMultipleMatchTag: &reference, SkipSingleNamePerformerTag: &reference,
				FieldOptions: []*identify.FieldOptions{{Field: "performers", Strategy: identify.FieldStrategyMerge, CreateMissing: &yes}},
			}
			results := []*models.ScrapedScene{{}, {}}
			if singleName {
				results = []*models.ScrapedScene{{Performers: []*models.ScrapedPerformer{{Name: &name}}}}
			}
			identifier := &identify.SceneIdentifier{
				TxnManager: repo.TxnManager, SceneReaderUpdater: repo.Scene, StudioReaderWriter: repo.Studio,
				PerformerCreator: repo.Performer, TagFinderCreator: repo.Tag, DefaultOptions: options,
				Sources:                     []identify.ScraperSource{{Scraper: fixedScraper{results}}},
				SceneUpdatePostHookExecutor: quietHooks{},
			}
			err := identifier.Identify(ctx, &scene)
			if reference != tagUUID {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
				ids, err := repo.Scene.GetTagIDs(ctx, scene.ID)
				require.NoError(t, err)
				require.Equal(t, []int{survivor.ID}, ids)
				return nil
			}))
		}
	}
	// The selected UUID must never follow a reused integer key after deletion.
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		if err := repo.Tag.Destroy(ctx, survivor.ID); err != nil {
			return err
		}
		_, _, err := db.ExecSQL(ctx, `INSERT INTO tags(id,name,created_at,updated_at) VALUES(?,'Replacement',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, []any{survivor.ID})
		return err
	}))
	identifier := &identify.SceneIdentifier{
		TxnManager: repo.TxnManager, SceneReaderUpdater: repo.Scene,
		DefaultOptions: &identify.MetadataOptions{SkipMultipleMatches: &yes, SkipMultipleMatchTag: &tagUUID},
		Sources:        []identify.ScraperSource{{Scraper: fixedScraper{[]*models.ScrapedScene{{}, {}}}}},
	}
	require.Error(t, identifier.Identify(ctx, &scene))
	require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
		ids, err := repo.Scene.GetTagIDs(ctx, scene.ID)
		require.NoError(t, err)
		require.Empty(t, ids)
		return nil
	}))
}
