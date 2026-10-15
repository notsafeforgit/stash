package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/image"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/scene"
	"github.com/stretchr/testify/require"
)

type galleryDeletionFixture struct {
	repo      models.Repository
	r         *Resolver
	hooks     *notificationRecorder
	dir       string
	folder    *models.Folder
	zip       *models.BaseFile
	zipFolder *models.Folder
}

func newGalleryDeletionFixture(t *testing.T) *galleryDeletionFixture {
	t.Helper()
	repo := pluginNotificationRepository(t)
	dir := t.TempDir()
	p := paths.NewPaths(filepath.Join(dir, "generated"), filepath.Join(dir, "blobs"))
	manager.SetInstance(&manager.Manager{Config: config.GetInstance(), Paths: &p, ReadLockManager: fsutil.NewReadLockManager()})
	t.Cleanup(func() { manager.SetInstance(nil) })
	f := &galleryDeletionFixture{repo: repo, dir: dir, folder: &models.Folder{Path: dir}, hooks: &notificationRecorder{}}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Folder.Create(ctx, f.folder) }))
	f.r = &Resolver{repository: repo, hookExecutor: f.hooks,
		imageService: &image.Service{Repository: repo.Image, File: repo.File},
		sceneService: &scene.Service{Repository: repo.Scene, File: repo.File, MarkerRepository: repo.SceneMarker}}
	return f
}

func (f *galleryDeletionFixture) addZip(t *testing.T) {
	t.Helper()
	f.zip = &models.BaseFile{Basename: "album.zip", ParentFolderID: f.folder.ID, Path: filepath.Join(f.dir, "album.zip")}
	require.NoError(t, os.WriteFile(f.zip.Path, []byte("archive fixture"), 0600))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := f.repo.File.Create(ctx, f.zip); err != nil {
			return err
		}
		f.zipFolder = &models.Folder{Path: f.zip.Path, ZipFileID: &f.zip.ID, ParentFolderID: &f.folder.ID}
		return f.repo.Folder.Create(ctx, f.zipFolder)
	}))
}

func (f *galleryDeletionFixture) gallery(t *testing.T, kind string) *models.Gallery {
	t.Helper()
	g := models.NewGallery()
	g.Title = kind
	input := &models.CreateGalleryInput{Gallery: &g}
	switch kind {
	case "source":
		g.Origin = models.GalleryOriginSource
	case "folder":
		g.FolderID = &f.folder.ID
	case "zip":
		input.FileIDs = []models.FileID{f.zip.ID}
	}
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.Gallery.Create(ctx, input) }))
	return &g
}

func (f *galleryDeletionFixture) image(t *testing.T, name string, zipped bool, galleries ...int) (*models.Image, string) {
	t.Helper()
	parent := f.folder
	if zipped {
		parent = f.zipFolder
	}
	file := &models.ImageFile{BaseFile: &models.BaseFile{Basename: name, ParentFolderID: parent.ID, Path: filepath.Join(parent.Path, name)}}
	if zipped {
		file.ZipFileID = &f.zip.ID
	} else {
		require.NoError(t, os.WriteFile(file.Path, []byte("image fixture"), 0600))
	}
	i := models.NewImage()
	i.GalleryIDs = models.NewRelatedIDs(galleries)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := f.repo.File.Create(ctx, file); err != nil {
			return err
		}
		return f.repo.Image.Create(ctx, &models.CreateImageInput{Image: &i, FileIDs: []models.FileID{file.ID}})
	}))
	return &i, file.Path
}

func (f *galleryDeletionFixture) scene(t *testing.T, galleries ...int) (*models.Scene, string) {
	t.Helper()
	file := &models.VideoFile{BaseFile: &models.BaseFile{Basename: "clip.mp4", ParentFolderID: f.folder.ID, Path: filepath.Join(f.dir, "clip.mp4")}}
	require.NoError(t, os.WriteFile(file.Path, []byte("video fixture"), 0600))
	s := models.NewScene()
	s.GalleryIDs = models.NewRelatedIDs(galleries)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := f.repo.File.Create(ctx, file); err != nil {
			return err
		}
		return f.repo.Scene.Create(ctx, &s, []models.FileID{file.ID})
	}))
	return &s, file.Path
}

func (f *galleryDeletionFixture) assertMedia(t *testing.T, imageID, sceneID int, exists bool) {
	t.Helper()
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		i, err := f.repo.Image.Find(ctx, imageID)
		require.NoError(t, err)
		s, err := f.repo.Scene.Find(ctx, sceneID)
		require.NoError(t, err)
		if exists {
			require.NotNil(t, i)
			require.NotNil(t, s)
		} else {
			require.Nil(t, i)
			require.Nil(t, s)
		}
		return nil
	}))
}

func TestGalleryDeletionAssociationOnlyPreservesAllMedia(t *testing.T) {
	for _, kind := range []string{"manual", "source", "folder", "zip"} {
		t.Run(kind, func(t *testing.T) {
			f := newGalleryDeletionFixture(t)
			if kind == "zip" {
				f.addZip(t)
			}
			g := f.gallery(t, kind)
			i, imagePath := f.image(t, "keep.jpg", kind == "zip", g.ID)
			s, scenePath := f.scene(t, g.ID)
			ok, err := f.r.Mutation().GalleryDestroy(t.Context(), models.GalleryDestroyInput{Ids: []string{strconv.Itoa(g.ID)}, DeleteGenerated: new(true)})
			require.NoError(t, err)
			require.True(t, ok)
			f.assertMedia(t, i.ID, s.ID, true)
			require.FileExists(t, scenePath)
			if kind == "zip" {
				require.FileExists(t, f.zip.Path)
			} else {
				require.FileExists(t, imagePath)
			}
			require.Len(t, f.hooks.calls, 1)
			require.Equal(t, hook.GalleryDestroyPost, f.hooks.calls[0].kind)
		})
	}
}

func TestGalleryDeletionFolderOnlyRemovesMembersAndEmptyBackingDirectory(t *testing.T) {
	for _, keepOtherFile := range []bool{false, true} {
		t.Run(strconv.FormatBool(keepOtherFile), func(t *testing.T) {
			f := newGalleryDeletionFixture(t)
			g := f.gallery(t, "folder")
			i, imagePath := f.image(t, "member.jpg", false, g.ID)
			s, scenePath := f.scene(t, g.ID)
			otherPath := filepath.Join(f.dir, "unrelated.txt")
			if keepOtherFile {
				require.NoError(t, os.WriteFile(otherPath, []byte("keep"), 0600))
			}
			ok, err := f.r.Mutation().GalleryDestroy(t.Context(), models.GalleryDestroyInput{Ids: []string{strconv.Itoa(g.ID)}, DeleteMedia: true, DeleteFile: new(true)})
			require.NoError(t, err)
			require.True(t, ok)
			f.assertMedia(t, i.ID, s.ID, false)
			require.NoFileExists(t, imagePath)
			require.NoFileExists(t, scenePath)
			if keepOtherFile {
				require.FileExists(t, otherPath)
				require.DirExists(t, f.dir)
			} else {
				require.NoDirExists(t, f.dir)
			}
		})
	}
}

func TestGalleryDeletionMixedMediaAndSharedSelection(t *testing.T) {
	for _, mode := range []string{"keep files", "delete files", "keep shared", "delete shared", "select both", "invalid files only"} {
		t.Run(mode, func(t *testing.T) {
			f := newGalleryDeletionFixture(t)
			g, other := f.gallery(t, "source"), f.gallery(t, "manual")
			galleryIDs := []int{g.ID}
			shared := mode == "keep shared" || mode == "delete shared" || mode == "select both"
			if shared {
				galleryIDs = append(galleryIDs, other.ID)
			}
			i, imagePath := f.image(t, "member.jpg", false, galleryIDs...)
			s, scenePath := f.scene(t, galleryIDs...)
			unrelated, unrelatedPath := f.image(t, "unrelated.jpg", false)
			ids := []string{strconv.Itoa(g.ID), strconv.Itoa(g.ID)}
			if mode == "select both" {
				ids = append(ids, strconv.Itoa(other.ID))
			}
			preview, err := f.r.Query().GalleryDeletePreview(t.Context(), ids)
			require.NoError(t, err)
			require.Equal(t, 1, preview.ImageCount)
			require.Equal(t, 1, preview.SceneCount)
			if shared && mode != "select both" {
				require.Equal(t, 1, preview.SharedImageCount)
				require.Equal(t, 1, preview.SharedSceneCount)
			} else {
				require.Zero(t, preview.SharedImageCount)
				require.Zero(t, preview.SharedSceneCount)
			}
			f.hooks.check = func(ctx context.Context, _ notificationCall) {
				require.NoError(t, f.repo.WithReadTxn(ctx, func(ctx context.Context) error {
					found, err := f.repo.Gallery.Find(ctx, g.ID)
					require.NoError(t, err)
					require.Nil(t, found)
					return nil
				}))
			}
			ok, err := f.r.Mutation().GalleryDestroy(t.Context(), models.GalleryDestroyInput{
				Ids: ids, DeleteMedia: mode != "invalid files only", DeleteSharedMedia: mode == "delete shared",
				DeleteFile: new(mode != "keep files"), DeleteGenerated: new(true),
			})
			if mode == "invalid files only" {
				require.ErrorContains(t, err, "requires delete_media")
				require.False(t, ok)
				require.Empty(t, f.hooks.calls)
				f.assertMedia(t, i.ID, s.ID, true)
				return
			}
			require.NoError(t, err)
			require.True(t, ok)
			kept := mode == "keep shared"
			f.assertMedia(t, i.ID, s.ID, kept)
			if kept || mode == "keep files" {
				require.FileExists(t, imagePath)
				require.FileExists(t, scenePath)
			} else {
				require.NoFileExists(t, imagePath)
				require.NoFileExists(t, scenePath)
			}
			require.FileExists(t, unrelatedPath)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				found, err := f.repo.Image.Find(ctx, unrelated.ID)
				require.NotNil(t, found)
				return err
			}))
			counts := map[hook.TriggerEnum]int{}
			for _, call := range f.hooks.calls {
				counts[call.kind]++
			}
			if kept {
				require.Zero(t, counts[hook.ImageDestroyPost])
				require.Zero(t, counts[hook.SceneDestroyPost])
			} else {
				require.Equal(t, 1, counts[hook.ImageDestroyPost])
				require.Equal(t, 1, counts[hook.SceneDestroyPost])
			}
			if mode == "select both" {
				require.Equal(t, 2, counts[hook.GalleryDestroyPost])
			} else {
				require.Equal(t, 1, counts[hook.GalleryDestroyPost])
			}
		})
	}
}

func TestGalleryDeletionZIPRetainsSurvivingContentsAndOwners(t *testing.T) {
	for _, mode := range []string{"delete archive", "shared archive", "shared image", "unselected image"} {
		t.Run(mode, func(t *testing.T) {
			f := newGalleryDeletionFixture(t)
			f.addZip(t)
			g := f.gallery(t, "zip")
			galleries := []int{g.ID}
			if mode == "shared archive" {
				f.gallery(t, "zip")
			}
			if mode == "shared image" {
				galleries = append(galleries, f.gallery(t, "manual").ID)
			}
			f.image(t, "member.jpg", true, galleries...)
			if mode == "unselected image" {
				f.image(t, "keep.jpg", true)
			}
			ok, err := f.r.Mutation().GalleryDestroy(t.Context(), models.GalleryDestroyInput{Ids: []string{strconv.Itoa(g.ID)}, DeleteMedia: true, DeleteFile: new(true)})
			require.NoError(t, err)
			require.True(t, ok)
			if mode == "delete archive" {
				require.NoFileExists(t, f.zip.Path)
			} else {
				require.FileExists(t, f.zip.Path)
			}
		})
	}
}

type failingGalleryDestroy struct{ models.GalleryReaderWriter }

func (f failingGalleryDestroy) Destroy(context.Context, int) error {
	return errors.New("injected gallery deletion failure")
}

func TestGalleryDeletionRollbackPreservesFilesAndEmitsNoHooks(t *testing.T) {
	f := newGalleryDeletionFixture(t)
	g := f.gallery(t, "source")
	i, imagePath := f.image(t, "member.jpg", false, g.ID)
	s, scenePath := f.scene(t, g.ID)
	f.r.repository.Gallery = failingGalleryDestroy{f.repo.Gallery}
	ok, err := f.r.Mutation().GalleryDestroy(t.Context(), models.GalleryDestroyInput{Ids: []string{strconv.Itoa(g.ID)}, DeleteMedia: true, DeleteFile: new(true)})
	require.ErrorContains(t, err, "injected gallery deletion failure")
	require.False(t, ok)
	require.Empty(t, f.hooks.calls)
	f.assertMedia(t, i.ID, s.ID, true)
	require.FileExists(t, imagePath)
	require.FileExists(t, scenePath)
}

func TestGalleryDeletionPreviewAndMutationUseUUIDs(t *testing.T) {
	f := newGalleryDeletionFixture(t)
	g := f.gallery(t, "source")
	i, _ := f.image(t, "member.jpg", false, g.ID)
	s, _ := f.scene(t, g.ID)
	call := nativeResolverCaller(t, f.r)
	variables := map[string]any{"ids": []string{libraryTestUUID(t, f.repo, models.ArchiveGallery, g.ID)}}
	require.JSONEq(t, `{"data":{"galleryDeletePreview":{"image_count":1,"scene_count":1,"shared_image_count":0}}}`, call(`query($ids:[ID!]!){galleryDeletePreview(ids:$ids){image_count scene_count shared_image_count}}`, variables))
	require.JSONEq(t, `{"data":{"galleryDestroy":true}}`, call(`mutation($ids:[ID!]!){galleryDestroy(input:{ids:$ids})}`, variables))
	f.assertMedia(t, i.ID, s.ID, true)
}
