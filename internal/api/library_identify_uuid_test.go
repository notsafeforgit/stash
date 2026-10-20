package api

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestIdentifyUUIDConfigurationGraphQL(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "identify.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	tag := models.NewTag()
	tag.Name = "Review"
	scene := models.NewScene()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Tag.Create(ctx, &models.CreateTagInput{Tag: &tag}); err != nil {
			return err
		}
		return repo.Scene.Create(ctx, &scene, nil)
	}))
	tagID := libraryTestUUID(t, repo, models.ArchiveTag, tag.ID)
	sceneID := libraryTestUUID(t, repo, models.ArchiveScene, scene.ID)
	r, c, _ := newSettingsResolver(t)
	r.repository = repo
	call := nativeResolverCaller(t, r.Resolver)
	const mutation = `mutation($input:IdentifyMetadataInput!){configureDefaults(input:{identify:$input}){identify{options{skipMultipleMatchTag} sources{options{skipSingleNamePerformerTag}}}}}`
	input := map[string]any{
		"sceneIDs": []string{sceneID},
		"options":  map[string]any{"skipMultipleMatchTag": tagID},
		"sources": []any{map[string]any{
			"source":  map[string]any{"scraper_id": "test"},
			"options": map[string]any{"skipSingleNamePerformerTag": tagID},
		}},
	}
	result := call(mutation, map[string]any{"input": input})
	require.NotContains(t, result, `"errors"`, result)
	require.Contains(t, result, tagID)
	options := c.GetDefaultIdentifySettings()
	require.Equal(t, []string{sceneID}, options.SceneIDs)
	require.Equal(t, tagID, *options.Options.SkipMultipleMatchTag)
	require.Equal(t, tagID, *options.Sources[0].Options.SkipSingleNamePerformerTag)
	for _, invalid := range []any{"1", 1, sceneID} {
		input["options"] = map[string]any{"skipMultipleMatchTag": invalid}
		result = call(mutation, map[string]any{"input": input})
		require.Contains(t, result, `"errors"`, result)
		require.Equal(t, tagID, *c.GetDefaultIdentifySettings().Options.SkipMultipleMatchTag)
	}
	result = call(`mutation { configureDefaults(input:{autoTag:{tags:["1"]}}){autoTag{tags}} }`, nil)
	require.Contains(t, result, `"errors"`, result)
}
