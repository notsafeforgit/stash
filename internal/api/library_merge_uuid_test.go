package api

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPerformerMergeUUIDGraphQL(t *testing.T) {
	for _, selection := range []string{"source", "destination", "clear"} {
		t.Run(selection, func(t *testing.T) { testPerformerMergeUUID(t, selection) })
	}
}

func testPerformerMergeUUID(t *testing.T, selection string) {
	t.Helper()
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	db := sqlite.NewDatabase()
	db.SetBlobStoreOptions(sqlite.BlobStoreOptions{UseDatabase: true})
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "merge.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	source, destination, unrelated := models.NewPerformer(), models.NewPerformer(), models.NewPerformer()
	source.Name, destination.Name = "Merge source", "Merge destination"
	unrelated.Name = "Unrelated performer"
	source.Details = "Retained source details"
	sourceImage := []byte("stored performer photo")
	destinationImage := []byte("original destination photo")
	tag := models.NewTag()
	tag.Name = "Selected tag"
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Tag.Create(ctx, &models.CreateTagInput{Tag: &tag}); err != nil {
			return err
		}
		for _, p := range []*models.Performer{&source, &destination, &unrelated} {
			if err := repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: p}); err != nil {
				return err
			}
		}
		if err := repo.Performer.UpdateImage(ctx, destination.ID, destinationImage); err != nil {
			return err
		}
		return repo.Performer.UpdateImage(ctx, source.ID, sourceImage)
	}))
	sourceUUID := libraryTestUUID(t, repo, models.ArchivePerformer, source.ID)
	destinationUUID := libraryTestUUID(t, repo, models.ArchivePerformer, destination.ID)
	unrelatedUUID := libraryTestUUID(t, repo, models.ArchivePerformer, unrelated.ID)
	tagUUID := libraryTestUUID(t, repo, models.ArchiveTag, tag.ID)
	call := nativeResolverCaller(t, &Resolver{repository: repo, hookExecutor: &mockHookExecutor{}})
	copyResult := call(`mutation($id:ID!,$source:ID!){performerUpdate(input:{id:$id,image_input:{performer_id:$source}}){id}}`,
		map[string]any{"id": unrelatedUUID, "source": sourceUUID})
	require.NotContains(t, copyResult, `"errors"`, copyResult)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		image, err := repo.Performer.GetImage(ctx, unrelated.ID)
		require.NoError(t, err)
		require.Equal(t, sourceImage, image)
		return nil
	}))
	const mutation = `mutation($input:PerformerMergeInput!){performerMerge(input:$input){id name details tags{id} aliases{alias}}}`
	image := map[string]any{"performer_id": sourceUUID}
	expectedImage := sourceImage
	switch selection {
	case "destination":
		image["performer_id"], expectedImage = destinationUUID, destinationImage
	case "clear":
		image, expectedImage = map[string]any{"data": ""}, nil
	}
	values := map[string]any{
		"id": destinationUUID, "name": source.Name, "details": source.Details,
		"image_input": image, "tag_ids": []string{tagUUID},
	}
	input := map[string]any{
		"source": []string{sourceUUID}, "destination": destinationUUID,
		"require_resolved_values": true, "values": values,
	}
	for _, invalid := range []any{
		map[string]any{"performer_id": "1"},
		map[string]any{"performer_id": source.ID},
		map[string]any{"performer_id": tagUUID},
		map[string]any{"performer_id": unrelatedUUID},
		map[string]any{"performer_id": sourceUUID, "data": ""},
	} {
		values["image_input"] = invalid
		result := call(mutation, map[string]any{"input": input})
		require.Contains(t, result, `"errors"`, result)
	}
	values["image_input"] = image
	values["id"] = sourceUUID
	require.Contains(t, call(mutation, map[string]any{"input": input}), "values id must match destination")
	values["id"] = destinationUUID
	result := call(mutation, map[string]any{"input": input})
	require.NotContains(t, result, `"errors"`, result)
	var response struct {
		Data struct {
			PerformerMerge struct {
				ID, Name, Details string
				Tags              []struct{ ID string }
				Aliases           []struct{ Alias string }
			}
		}
	}
	require.NoError(t, json.Unmarshal([]byte(result), &response))
	merged := response.Data.PerformerMerge
	require.Equal(t, destinationUUID, merged.ID)
	require.Equal(t, source.Name, merged.Name)
	require.Equal(t, source.Details, merged.Details)
	require.Equal(t, []struct{ ID string }{{tagUUID}}, merged.Tags)
	require.Contains(t, merged.Aliases, struct{ Alias string }{destination.Name})
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		image, err := repo.Performer.GetImage(ctx, destination.ID)
		require.NoError(t, err)
		require.Equal(t, expectedImage, image)
		merged, err := repo.ArchiveEntity.Resolve(ctx, sourceUUID)
		require.NoError(t, err)
		require.Equal(t, destinationUUID, merged.UUID)
		return nil
	}))
}
