package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestGalleryCoverAcceptsOneMemberAndNotifiesAfterCommit(t *testing.T) {
	repo := pluginNotificationRepository(t)
	g, image, scene, unrelated := models.NewGallery(), models.NewImage(), models.NewScene(), models.NewScene()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		require.NoError(t, repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &g}))
		require.NoError(t, repo.Image.Create(ctx, &models.CreateImageInput{Image: &image}))
		require.NoError(t, repo.Scene.Create(ctx, &scene, nil))
		require.NoError(t, repo.Scene.Create(ctx, &unrelated, nil))
		require.NoError(t, repo.Gallery.AddImages(ctx, g.ID, image.ID))
		return repo.Gallery.AddSceneIDs(ctx, g.ID, []int{scene.ID})
	}))
	hooks := &notificationRecorder{}
	resolver := &Resolver{repository: repo, hookExecutor: hooks, galleryService: &gallery.Service{Repository: repo.Gallery}}
	mutation := resolver.Mutation()
	galleryID, imageID, sceneID, unrelatedID := strconv.Itoa(g.ID), strconv.Itoa(image.ID), strconv.Itoa(scene.ID), strconv.Itoa(unrelated.ID)
	for _, input := range []GallerySetCoverInput{
		{GalleryID: galleryID, ImageID: &imageID},
		{GalleryID: galleryID, SceneID: &sceneID},
	} {
		hooks.check = func(ctx context.Context, call notificationCall) {
			require.Equal(t, g.ID, call.id)
			require.Equal(t, hook.GalleryUpdatePost, call.kind)
			require.Equal(t, []string{"cover"}, call.fields)
			require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
				stored, err := repo.Gallery.Find(ctx, g.ID)
				require.NoError(t, err)
				cover, err := gallery.FindCover(ctx, repo, stored, "cover")
				require.NoError(t, err)
				if input.ImageID != nil {
					require.Equal(t, image.ID, cover.Image.ID)
					require.Nil(t, cover.Scene)
				} else {
					require.Equal(t, scene.ID, cover.Scene.ID)
					require.Nil(t, cover.Image)
				}
				return nil
			}))
		}
		ok, err := mutation.SetGalleryCover(t.Context(), input)
		require.NoError(t, err)
		require.True(t, ok)
	}
	require.Len(t, hooks.calls, 2)
	hooks.calls = nil
	for _, input := range []GallerySetCoverInput{
		{GalleryID: galleryID},
		{GalleryID: galleryID, ImageID: &imageID, SceneID: &sceneID},
		{GalleryID: galleryID, SceneID: &unrelatedID},
		{GalleryID: galleryID, SceneID: PtrString("-1")},
		{GalleryID: "999999", SceneID: &sceneID},
	} {
		ok, err := mutation.SetGalleryCover(t.Context(), input)
		require.Error(t, err)
		require.False(t, ok)
		require.Empty(t, hooks.calls)
	}
	hooks.check = nil
	var stored *models.Gallery
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		stored, err = repo.Gallery.Find(ctx, g.ID)
		return err
	}))
	cover, err := resolver.Gallery().Cover(t.Context(), stored)
	require.NoError(t, err)
	require.Equal(t, scene.ID, cover.Scene.ID, "rejected writes retain the last successful selection")
	call := nativeResolverCaller(t, resolver)
	require.JSONEq(t, `{"data":{"findGallery":{"cover":{"image":null,"scene":{"id":"`+sceneID+`"}}}}}`,
		call(`query($id:ID!){findGallery(id:$id){cover{image{id} scene{id}}}}`, map[string]any{"id": galleryID}))
	ok, err := mutation.ResetGalleryCover(t.Context(), GalleryResetCoverInput{GalleryID: galleryID})
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, hooks.calls, 1)
	require.Equal(t, []string{"cover"}, hooks.calls[0].fields)
}

func TestGallerySceneCoverServesUpdatedArtworkAtTheSameGalleryURL(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	db := sqlite.NewDatabase()
	db.SetBlobStoreOptions(sqlite.BlobStoreOptions{UseDatabase: true})
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "covers.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	g, scene := models.NewGallery(), models.NewScene()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		require.NoError(t, repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &g}))
		require.NoError(t, repo.Scene.Create(ctx, &scene, nil))
		require.NoError(t, repo.Gallery.AddSceneIDs(ctx, g.ID, []int{scene.ID}))
		identity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveScene, scene.ID)
		require.NoError(t, err)
		return repo.Gallery.SetCover(ctx, g.ID, identity.UUID)
	}))
	rs := galleryRoutes{routes: routes{txnManager: repo.TxnManager}, repository: repo, galleryFinder: repo.Gallery, fileGetter: repo.File}
	var lastETag string
	for _, color := range []string{"red", "blue"} {
		artwork := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10" fill="` + color + `"/></svg>`)
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			return repo.Scene.UpdateCover(ctx, scene.ID, artwork)
		}))
		request := httptest.NewRequest(http.MethodGet, "/"+strconv.Itoa(g.ID)+"/cover?t=old-gallery-timestamp", nil)
		if lastETag != "" {
			request.Header.Set("If-None-Match", lastETag)
		}
		response := httptest.NewRecorder()
		rs.Routes().ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, artwork, response.Body.Bytes())
		require.NotContains(t, response.Header().Get("Cache-Control"), "immutable")
		require.NotEmpty(t, response.Header().Get("ETag"))
		require.NotEqual(t, lastETag, response.Header().Get("ETag"))
		lastETag = response.Header().Get("ETag")
	}
}
