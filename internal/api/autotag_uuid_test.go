package api

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type failingAutoTagStudio struct{ models.StudioReaderWriter }

func (s failingAutoTagStudio) Find(context.Context, int) (*models.Studio, error) {
	return nil, errors.New("studio lookup failed")
}

func TestAutoTagUUIDGraphQL(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "autotag.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	jobs := job.NewManager()
	jobs.SetSync(true)
	t.Cleanup(jobs.Stop)
	mgr := &manager.Manager{Repository: repo, JobManager: jobs}
	manager.SetInstance(mgr)
	t.Cleanup(func() { manager.SetInstance(nil); config.InitializeEmpty() })
	studio := models.NewCreateStudioInput()
	studio.Name = "Studio canonical"
	studio.Aliases = models.NewRelatedStrings([]string{"Studio alias"})
	otherStudio := models.NewCreateStudioInput()
	otherStudio.Name = "Existing studio"
	performer := models.NewPerformer()
	performer.Name = "Performer canonical"
	performer.Aliases = models.NewRelatedPerformerAliases([]models.PerformerAlias{{Alias: "Performer alias"}})
	tag := models.NewTag()
	tag.Name = "Tag canonical"
	tag.Aliases = models.NewRelatedStrings([]string{"Tag alias"})
	var scenes, images, galleries []int
	var organizedID, existingID int
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		require.NoError(t, repo.Studio.Create(ctx, &studio))
		require.NoError(t, repo.Studio.Create(ctx, &otherStudio))
		require.NoError(t, repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &performer}))
		require.NoError(t, repo.Tag.Create(ctx, &models.CreateTagInput{Tag: &tag}))
		for _, suffix := range []string{"canonical", "alias"} {
			folder := models.Folder{Path: "/media/Studio " + suffix + "/Performer " + suffix + "/Tag " + suffix}
			require.NoError(t, repo.Folder.Create(ctx, &folder))
			video := &models.VideoFile{BaseFile: &models.BaseFile{ParentFolderID: folder.ID, Basename: "clip.mp4"}}
			require.NoError(t, repo.File.Create(ctx, video))
			scene := models.NewScene()
			require.NoError(t, repo.Scene.Create(ctx, &scene, []models.FileID{video.ID}))
			scenes = append(scenes, scene.ID)
			imageFile := &models.ImageFile{BaseFile: &models.BaseFile{ParentFolderID: folder.ID, Basename: "picture.jpg"}}
			require.NoError(t, repo.File.Create(ctx, imageFile))
			image := models.NewImage()
			require.NoError(t, repo.Image.Create(ctx, &models.CreateImageInput{Image: &image, FileIDs: []models.FileID{imageFile.ID}}))
			images = append(images, image.ID)
			zip := &models.BaseFile{ParentFolderID: folder.ID, Basename: "album.zip"}
			require.NoError(t, repo.File.Create(ctx, zip))
			gallery := models.NewGallery()
			require.NoError(t, repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &gallery, FileIDs: []models.FileID{zip.ID}}))
			galleries = append(galleries, gallery.ID)
			if suffix == "alias" {
				for _, organized := range []bool{false, true} {
					file := &models.VideoFile{BaseFile: &models.BaseFile{ParentFolderID: folder.ID, Basename: strconv.FormatBool(organized) + ".mp4"}}
					require.NoError(t, repo.File.Create(ctx, file))
					scene := models.NewScene()
					scene.Organized = organized
					if !organized {
						scene.StudioID = &otherStudio.ID
					}
					require.NoError(t, repo.Scene.Create(ctx, &scene, []models.FileID{file.ID}))
					if organized {
						organizedID = scene.ID
					} else {
						existingID = scene.ID
					}
				}
			}
		}
		return nil
	}))
	public := map[string]string{
		"performers": libraryTestUUID(t, repo, models.ArchivePerformer, performer.ID),
		"studios":    libraryTestUUID(t, repo, models.ArchiveStudio, studio.ID),
		"tags":       libraryTestUUID(t, repo, models.ArchiveTag, tag.ID),
	}
	call := nativeResolverCaller(t, &Resolver{repository: repo})
	mutation := `mutation($input:AutoTagMetadataInput!){metadataAutoTag(input:$input)}`
	run := func(input map[string]any) *job.Job {
		t.Helper()
		result := call(mutation, map[string]any{"input": input})
		require.NotContains(t, result, `"errors"`, result)
		var response struct {
			Data struct {
				ID string `json:"metadataAutoTag"`
			}
		}
		require.NoError(t, json.Unmarshal([]byte(result), &response))
		require.Len(t, response.Data.ID, 36)
		return jobs.GetJob(response.Data.ID)
	}
	for _, field := range []string{"performers", "studios", "tags"} {
		t.Run(field, func(t *testing.T) {
			state := run(map[string]any{field: []string{public[field]}})
			require.Equal(t, job.StatusFinished, state.Status)
			require.Nil(t, state.Error)
		})
	}
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, id := range scenes {
			scene, err := repo.Scene.Find(ctx, id)
			require.NoError(t, err)
			require.Equal(t, &studio.ID, scene.StudioID)
			require.NoError(t, scene.LoadPerformerIDs(ctx, repo.Scene))
			require.NoError(t, scene.LoadTagIDs(ctx, repo.Scene))
			require.Equal(t, []int{performer.ID}, scene.PerformerIDs.List())
			require.Equal(t, []int{tag.ID}, scene.TagIDs.List())
		}
		for _, id := range images {
			image, err := repo.Image.Find(ctx, id)
			require.NoError(t, err)
			require.Equal(t, &studio.ID, image.StudioID)
			require.NoError(t, image.LoadPerformerIDs(ctx, repo.Image))
			require.NoError(t, image.LoadTagIDs(ctx, repo.Image))
			require.Equal(t, []int{performer.ID}, image.PerformerIDs.List())
			require.Equal(t, []int{tag.ID}, image.TagIDs.List())
		}
		for _, id := range galleries {
			gallery, err := repo.Gallery.Find(ctx, id)
			require.NoError(t, err)
			require.Equal(t, &studio.ID, gallery.StudioID)
			require.NoError(t, gallery.LoadPerformerIDs(ctx, repo.Gallery))
			require.NoError(t, gallery.LoadTagIDs(ctx, repo.Gallery))
			require.Equal(t, []int{performer.ID}, gallery.PerformerIDs.List())
			require.Equal(t, []int{tag.ID}, gallery.TagIDs.List())
		}
		organized, err := repo.Scene.Find(ctx, organizedID)
		require.NoError(t, err)
		require.Nil(t, organized.StudioID)
		existing, err := repo.Scene.Find(ctx, existingID)
		require.NoError(t, err)
		require.Equal(t, &otherStudio.ID, existing.StudioID)
		return nil
	}))
	t.Run("wildcards", func(t *testing.T) {
		state := run(map[string]any{"performers": []string{"*"}, "studios": []string{"*"}, "tags": []string{"*"}})
		require.Equal(t, job.StatusFinished, state.Status)
	})
	for _, invalid := range [][]string{
		{strconv.Itoa(studio.ID)}, {public["performers"]}, {uuid.NewString()},
		{"*", public["studios"]}, {public["studios"], "*"},
	} {
		result := call(mutation, map[string]any{"input": map[string]any{"studios": invalid}})
		require.Contains(t, result, `"errors"`, result)
	}
	t.Run("worker failure is visible", func(t *testing.T) {
		mgr.Repository.Studio = failingAutoTagStudio{repo.Studio}
		state := run(map[string]any{"studios": []string{public["studios"]}})
		require.Equal(t, job.StatusFailed, state.Status)
		require.NotNil(t, state.Error)
		require.Contains(t, *state.Error, "studio lookup failed")
	})
}
