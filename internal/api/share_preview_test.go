package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/internal/sharing"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/previewimage"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func sharedPreviewManager(t *testing.T, rs *shareRoutes) *manager.Manager {
	t.Helper()
	c := config.GetInstance()
	c.SetBool(config.EnableV3UI, true)
	c.SetString(config.Generated, t.TempDir())
	p := paths.NewPaths(c.GetGeneratedPath(), "")
	mgr := &manager.Manager{Config: c, Paths: &p}
	rs.server = &Server{manager: mgr}
	return mgr
}

func sharedPreviewRequest(handler http.Handler, method, url string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestShareGeneratedPreviewsStayScoped(t *testing.T) {
	rs, handler, other, otherSecret := shareHTTPMediaFixture(t, true)
	mgr := sharedPreviewManager(t, rs)
	snapshot, err := sharing.Snapshot(other)
	require.NoError(t, err)
	f, _, err := rs.service.Resolve(context.Background(), snapshot.Media[0])
	require.NoError(t, err)
	vf, ok := f.(*models.VideoFile)
	require.True(t, ok)
	scene := &models.Scene{ID: 3, CoverChecksum: "cover-checksum", Files: models.NewRelatedVideoFiles([]*models.VideoFile{vf})}
	m := mocks.NewDatabase()
	rs.service.Repo.Scene = m.Scene
	m.Scene.On("Find", mock.Anything, 3).Return(scene, nil)
	m.Scene.On("FindByIDs", mock.Anything, []int{3}).Return([]*models.Scene{scene}, nil)
	m.Scene.On("GetCover", mock.Anything, 3).Return([]byte("\xff\xd8\xfflegacy cover"), nil)
	row, secret, err := rs.service.Create(context.Background(), "owner", sharing.Options{Label: "Scene share", ExpiresAt: time.Now().Add(time.Hour)}, []sharing.Target{{Kind: "SCENE", ID: 3}})
	require.NoError(t, err)
	cookie := exchangeShareHTTP(t, handler, row, secret)
	otherCookie := exchangeShareHTTP(t, handler, other, otherSecret)
	base := "/share/" + row.ID + "/"
	mediaBase := base + "media/scene-3/"
	get := func(url string) *httptest.ResponseRecorder {
		return sharedPreviewRequest(handler, http.MethodGet, url, cookie)
	}
	content := func() publicShareContent {
		w := get(base + "content")
		require.Equal(t, http.StatusOK, w.Code)
		var ret publicShareContent
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ret))
		return ret
	}

	store := mgr.PreviewImageStore()
	require.NoError(t, os.MkdirAll(store.Root, 0o700))
	stage, err := os.MkdirTemp(store.Root, ".preview-")
	require.NoError(t, err)
	result := &previewimage.Result{Directory: stage, Variants: []previewimage.Variant{
		{File: "cover.jpg", MIMEType: "image/jpeg", DynamicRange: previewimage.SDR, Width: 1920, Height: 1080},
		{File: "cover.avif", MIMEType: "image/avif", DynamicRange: previewimage.Adaptive, Width: 1920, Height: 1080},
	}, Thumbnail: []previewimage.Variant{
		{File: "thumbnail.jpg", MIMEType: "image/jpeg", DynamicRange: previewimage.SDR, Width: 640, Height: 360},
		{File: "thumbnail.avif", MIMEType: "image/avif", DynamicRange: previewimage.HDR, Width: 640, Height: 360},
	}}
	t.Cleanup(result.Close)
	for _, variant := range append(result.Variants, result.Thumbnail...) {
		require.NoError(t, os.WriteFile(filepath.Join(stage, variant.File), []byte("unchanged "+variant.File), 0o600))
	}
	require.NoError(t, store.Publish(scene.ID, "cover", previewimage.CoverKey(scene.CoverChecksum), 1, result))

	require.Nil(t, content().Media[0].PreviewImage, "reuse must be opt-in")
	mgr.Config.SetBool(config.SharingUseExistingPreviews, true)
	preview := content().Media[0].PreviewImage
	require.NotNil(t, preview)
	require.NotNil(t, preview.Thumbnail)
	require.Equal(t, PreviewImageDynamicRangeAdaptive, preview.Sources[0].DynamicRange)
	require.Equal(t, PreviewImageDynamicRangeHdr, preview.Thumbnail.Sources[0].DynamicRange)
	require.Empty(t, content().Media[0].Download)
	for url, body := range map[string]string{
		preview.Fallback: "unchanged cover.jpg", preview.Sources[0].URL: "unchanged cover.avif",
		preview.Thumbnail.Fallback: "unchanged thumbnail.jpg", preview.Thumbnail.Sources[0].URL: "unchanged thumbnail.avif",
		mediaBase + "thumbnail": "unchanged thumbnail.jpg", mediaBase + "image": "unchanged cover.jpg",
	} {
		require.True(t, strings.HasPrefix(url, mediaBase))
		w := get(url)
		require.Equal(t, http.StatusOK, w.Code, url)
		require.Equal(t, body, w.Body.String())
		require.Contains(t, w.Header().Get("Cache-Control"), "no-store")
		require.NotContains(t, w.Header().Get("Cache-Control"), "immutable")
		require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, url, nil).Code)
		require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, url, otherCookie).Code)
	}
	assetURL := preview.Sources[0].URL
	require.Equal(t, "image/avif", get(assetURL).Header().Get("Content-Type"))
	head := sharedPreviewRequest(handler, http.MethodHead, assetURL, cookie)
	require.Equal(t, http.StatusOK, head.Code)
	require.Empty(t, head.Body.String())
	for _, url := range []string{
		mediaBase + "preview-image/manifest.json", assetURL + "-stale",
		strings.Replace(assetURL, "scene-3", "scene-4", 1),
		strings.Replace(assetURL, "scene-3", "image-1", 1), mediaBase + "download",
	} {
		require.Equal(t, http.StatusNotFound, get(url).Code, url)
	}

	mgr.Config.SetBool(config.SharingUseExistingPreviews, false)
	require.Nil(t, content().Media[0].PreviewImage)
	require.Equal(t, http.StatusNotFound, get(assetURL).Code, "previous catalog URLs must stop working when reuse is disabled")
	mgr.Config.SetBool(config.SharingUseExistingPreviews, true)
	oldID := vf.ID
	vf.ID++
	require.Equal(t, http.StatusNotFound, get(assetURL).Code, "a replacement primary file cannot inherit the grant")
	vf.ID = oldID
	oldCover := scene.CoverChecksum
	scene.CoverChecksum = "replacement cover"
	require.Equal(t, http.StatusNotFound, get(assetURL).Code, "an old manifest cannot resurrect a replaced cover")
	require.Equal(t, "\xff\xd8\xfflegacy cover", get(mediaBase+"thumbnail").Body.String())
	scene.CoverChecksum = oldCover
	require.NoError(t, rs.service.Revoke(context.Background(), row.ID))
	require.Equal(t, http.StatusNotFound, get(assetURL).Code)
}

func TestSharedImageThumbnailReuseDoesNotEnableOriginals(t *testing.T) {
	rs, handler, row, secret := shareHTTPFixture(t)
	mgr := sharedPreviewManager(t, rs)
	snapshot, err := sharing.Snapshot(row)
	require.NoError(t, err)
	f, _, err := rs.service.Resolve(context.Background(), snapshot.Media[0])
	require.NoError(t, err)
	m := mocks.NewDatabase()
	rs.service.Repo.Image = m.Image
	f.Base().Fingerprints = models.Fingerprints{{Type: models.FingerprintTypeMD5, Fingerprint: strings.Repeat("a", 32)}}
	img := &models.Image{ID: 1, Checksum: strings.Repeat("a", 32), Files: models.NewRelatedFiles([]models.File{f})}
	m.Image.On("Find", mock.Anything, 1).Return(img, nil)
	path := mgr.Paths.Generated.GetThumbnailPath(img.Checksum, models.DefaultGthumbWidth)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("stored image thumbnail"), 0o600))
	mgr.Config.SetBool(config.SharingUseExistingPreviews, true)
	cookie := exchangeShareHTTP(t, handler, row, secret)
	base := "/share/" + row.ID + "/media/image-1/"
	w := sharedPreviewRequest(handler, http.MethodGet, base+"thumbnail", cookie)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "stored image thumbnail", w.Body.String())
	require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, base+"download", cookie).Code)
	// Full-size views and missing thumbnails must return to the sanitizing path,
	// never the source file, even with preview reuse enabled.
	req := httptest.NewRequest(http.MethodGet, base+"image", nil)
	require.False(t, rs.existingRendition(httptest.NewRecorder(), req, &snapshot.Media[0], f, nil, false))
	require.NoError(t, os.Remove(path))
	require.False(t, rs.existingRendition(httptest.NewRecorder(), req, &snapshot.Media[0], f, nil, true))
	require.NoError(t, os.WriteFile(f.Base().Path, []byte("source changed"), 0o600))
	require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, base+"thumbnail", cookie).Code)
}

func TestSharingPreviewConfigurationPersistsAndPreservesOmittedOption(t *testing.T) {
	c := config.InitializeEmpty()
	c.SetConfigFile(filepath.Join(t.TempDir(), "config.yml"))
	r := &mutationResolver{}
	require.False(t, sharingConfiguration().UseExistingPreviews)
	enabled := true
	result, err := r.ConfigureSharing(context.Background(), "https://shares.test/share", &enabled)
	require.NoError(t, err)
	require.True(t, result.UseExistingPreviews)
	data, err := os.ReadFile(c.GetConfigFile())
	require.NoError(t, err)
	require.Contains(t, string(data), "sharing_use_existing_previews: true")
	result, err = r.ConfigureSharing(context.Background(), "https://other.test/share", nil)
	require.NoError(t, err)
	require.True(t, result.UseExistingPreviews)
	enabled = false
	_, err = r.ConfigureSharing(context.Background(), "http://invalid.test/share", &enabled)
	require.Error(t, err)
	require.True(t, sharingConfiguration().UseExistingPreviews)
	result, err = r.ConfigureSharing(context.Background(), "", &enabled)
	require.NoError(t, err)
	require.False(t, result.UseExistingPreviews)
}
