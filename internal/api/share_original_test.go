package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/internal/sharing"
	"github.com/stashapp/stash/internal/static"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestShareOriginalImagesKeepBytesAndScope(t *testing.T) {
	for _, test := range []struct{ ext, mime, data string }{
		{".jpg", "image/jpeg", "\xff\xd8\xff\xe1EXIF GPS and camera metadata\xff\xd9"},
		{".png", "image/png", "\x89PNG\r\n\x1a\noriginal PNG metadata"},
		{".gif", "image/gif", "GIF89aanimated original"},
		{".avif", "image/avif", "original HDR AVIF with metadata"},
		{".svg", "image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(document.cookie)</script></svg>`},
		{".html", "application/octet-stream", `<script>alert(document.cookie)</script>`},
	} {
		t.Run(test.ext, func(t *testing.T) {
			rs, handler, old, oldSecret := shareHTTPFixture(t)
			mgr := sharedPreviewManager(t, rs)
			snapshot, err := sharing.Snapshot(old)
			require.NoError(t, err)
			f, _, err := rs.service.Resolve(context.Background(), snapshot.Media[0])
			require.NoError(t, err)
			f.Base().Basename = "private-name" + test.ext
			require.NoError(t, os.WriteFile(f.Base().Path, []byte(test.data), 0o600))
			row, secret, err := rs.service.Create(context.Background(), "owner", sharing.Options{Label: "Original", ExpiresAt: time.Now().Add(time.Hour)}, []sharing.Target{{Kind: "IMAGE", ID: 1}})
			require.NoError(t, err)
			cookie := exchangeShareHTTP(t, handler, row, secret)
			otherCookie := exchangeShareHTTP(t, handler, old, oldSecret)
			base := "/share/" + row.ID + "/media/image-1/"
			mgr.Config.SetBool(config.SharingServeOriginalMedia, true)
			for _, endpoint := range []string{"image", "thumbnail"} {
				url := base + endpoint
				w := sharedPreviewRequest(handler, http.MethodGet, url, cookie)
				require.Equal(t, http.StatusOK, w.Code)
				require.Equal(t, test.data, w.Body.String(), "no resizing, conversion, animation loss or metadata stripping")
				require.Equal(t, test.mime, w.Header().Get("Content-Type"))
				require.Contains(t, w.Header().Get("Cache-Control"), "no-store")
				require.Contains(t, w.Header().Get("Content-Security-Policy"), "sandbox")
				require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
				require.NotContains(t, w.Header().Get("Content-Disposition"), "private-name")
				require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, url, nil).Code)
				require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, url, otherCookie).Code)
				head := sharedPreviewRequest(handler, http.MethodHead, url, cookie)
				require.Equal(t, http.StatusOK, head.Code)
				require.Empty(t, head.Body.String())
			}
			require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, base+"download", cookie).Code)
			require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, strings.Replace(base, "image-1", "image-2", 1)+"image", cookie).Code)
			// Generated thumbnails still take precedence, without changing full views.
			f.Base().Fingerprints = models.Fingerprints{{Type: models.FingerprintTypeMD5, Fingerprint: strings.Repeat("a", 32)}}
			thumbnail := mgr.Paths.Generated.GetThumbnailPath(strings.Repeat("a", 32), models.DefaultGthumbWidth)
			require.NoError(t, os.MkdirAll(filepath.Dir(thumbnail), 0o700))
			require.NoError(t, os.WriteFile(thumbnail, []byte("existing preview"), 0o600))
			require.Equal(t, "existing preview", sharedPreviewRequest(handler, http.MethodGet, base+"thumbnail", cookie).Body.String())
			require.Equal(t, test.data, sharedPreviewRequest(handler, http.MethodGet, base+"image", cookie).Body.String())
			f.Base().ID++
			require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, base+"image", cookie).Code)
			f.Base().ID--
			require.NoError(t, rs.service.Revoke(context.Background(), row.ID))
			require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, base+"image", cookie).Code)
			_, err = os.Stat(filepath.Join(mgr.Config.GetGeneratedPath(), "shares"))
			require.True(t, os.IsNotExist(err), "original delivery must not create a rendition cache")
		})
	}
}

func TestShareOriginalVideoSupportsRangesWithoutTranscoding(t *testing.T) {
	for _, kind := range []string{"SCENE", "IMAGE"} {
		t.Run(kind, func(t *testing.T) {
			rs, handler, old, _ := shareHTTPMediaFixture(t, true)
			mgr := sharedPreviewManager(t, rs)
			snapshot, err := sharing.Snapshot(old)
			require.NoError(t, err)
			f, _, err := rs.service.Resolve(context.Background(), snapshot.Media[0])
			require.NoError(t, err)
			vf := f.(*models.VideoFile)
			vf.Basename = "private-name.mp4"
			m := mocks.NewDatabase()
			rs.service.Repo.Scene = m.Scene
			scene := &models.Scene{ID: 1, Files: models.NewRelatedVideoFiles([]*models.VideoFile{vf})}
			m.Scene.On("Find", mock.Anything, 1).Return(scene, nil)
			m.Scene.On("GetCover", mock.Anything, 1).Return([]byte(nil), nil)
			row, secret, err := rs.service.Create(context.Background(), "owner", sharing.Options{Label: "Original video", ExpiresAt: time.Now().Add(time.Hour)}, []sharing.Target{{Kind: kind, ID: 1}})
			require.NoError(t, err)
			cookie := exchangeShareHTTP(t, handler, row, secret)
			base := "/share/" + row.ID + "/media/" + strings.ToLower(kind) + "-1/"
			get := func(endpoint string) *httptest.ResponseRecorder {
				return sharedPreviewRequest(handler, http.MethodGet, base+endpoint, cookie)
			}
			require.Equal(t, http.StatusNotFound, get("stream").Code)
			mgr.Config.SetBool(config.SharingUseExistingPreviews, true)
			require.Equal(t, http.StatusNotFound, get("stream").Code, "preview reuse alone must not enable originals")
			mgr.Config.SetBool(config.SharingServeOriginalMedia, true)
			w := get("")
			require.Equal(t, http.StatusOK, w.Code)
			var detail publicShareDetail
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
			require.Len(t, detail.Streams, 1)
			require.Equal(t, base+"stream", detail.Streams[0].URL)
			require.Equal(t, "video/mp4", *detail.Streams[0].MimeType)
			require.Empty(t, detail.Media.Download)
			require.Equal(t, "original image bytes", get("stream").Body.String())
			request := httptest.NewRequest(http.MethodGet, base+"stream", nil)
			request.AddCookie(cookie)
			request.Header.Set("Range", "bytes=9-13")
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, request)
			require.Equal(t, http.StatusPartialContent, w.Code)
			require.Equal(t, "image", w.Body.String())
			require.Equal(t, "bytes 9-13/20", w.Header().Get("Content-Range"))
			require.Contains(t, w.Header().Get("Cache-Control"), "no-store")
			require.Empty(t, sharedPreviewRequest(handler, http.MethodHead, base+"stream", cookie).Body.String())
			for _, endpoint := range []string{"stream.master.m3u8", "stream.fmp4.master.m3u8", "stream.fmp4.aac.master.m3u8", "stream.m3u8/video/0.m4s", "download"} {
				require.Equal(t, http.StatusNotFound, get(endpoint).Code, endpoint)
			}
			require.Equal(t, static.ReadAll(static.DefaultSceneImage), get("thumbnail").Body.Bytes(), "missing video covers must not start a conversion")
			mgr.Config.SetBool(config.SharingServeOriginalMedia, false)
			require.Equal(t, http.StatusNotFound, get("stream").Code)
			mgr.Config.SetBool(config.SharingServeOriginalMedia, true)
			require.NoError(t, os.WriteFile(vf.Path, []byte("source replaced"), 0o600))
			require.Equal(t, http.StatusNotFound, get("stream").Code)
		})
	}
}

func TestShareOriginalArchiveOnlyServesFrozenGalleryMembers(t *testing.T) {
	rs, handler, old, _ := shareHTTPFixture(t)
	mgr := sharedPreviewManager(t, rs)
	snapshot, err := sharing.Snapshot(old)
	require.NoError(t, err)
	f, _, err := rs.service.Resolve(context.Background(), snapshot.Media[0])
	require.NoError(t, err)
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	data := "unchanged image bytes with metadata"
	for name, content := range map[string]string{"shared.jpg": data, "private.jpg": "unshared image"} {
		member, err := zipWriter.Create(name)
		require.NoError(t, err)
		_, err = io.WriteString(member, content)
		require.NoError(t, err)
	}
	require.NoError(t, zipWriter.Close())
	zipPath := filepath.Join(t.TempDir(), "private-archive.zip")
	require.NoError(t, os.WriteFile(zipPath, archive.Bytes(), 0o600))
	zipFile := &models.BaseFile{ID: 7, Path: zipPath, Size: int64(archive.Len())}
	f.Base().DirEntry = models.DirEntry{ZipFileID: &zipFile.ID, ZipFile: zipFile}
	f.Base().Path = filepath.Join(zipPath, "shared.jpg")
	f.Base().Size = int64(len(data))
	m := mocks.NewDatabase()
	rs.service.Repo.Gallery = m.Gallery
	rs.service.Repo.Image = m.Image
	img := &models.Image{ID: 1, Files: models.NewRelatedFiles([]models.File{f})}
	m.Image.On("Find", mock.Anything, 1).Return(img, nil)
	m.Image.On("FindByGalleryID", mock.Anything, 4).Return([]*models.Image{img}, nil)
	m.Gallery.On("Find", mock.Anything, 4).Return(&models.Gallery{ID: 4}, nil)
	row, secret, err := rs.service.Create(context.Background(), "owner", sharing.Options{Label: "Gallery", ExpiresAt: time.Now().Add(time.Hour), AllowDownload: true}, []sharing.Target{{Kind: "GALLERY", ID: 4}})
	require.NoError(t, err)
	cookie := exchangeShareHTTP(t, handler, row, secret)
	mgr.Config.SetBool(config.SharingServeOriginalMedia, true)
	base := "/share/" + row.ID + "/media/image-1/"
	for _, endpoint := range []string{"image", "thumbnail", "download"} {
		w := sharedPreviewRequest(handler, http.MethodGet, base+endpoint, cookie)
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, data, w.Body.String())
		require.NotContains(t, w.Header().Get("Content-Disposition"), "private")
		head := sharedPreviewRequest(handler, http.MethodHead, base+endpoint, cookie)
		require.Equal(t, http.StatusOK, head.Code)
		require.Empty(t, head.Body.String())
		for _, seek := range []struct{ rangeHeader, expected string }{
			{"bytes=10-14", "image"}, {"bytes=0-8", "unchanged"}, {"bytes=-8", "metadata"},
		} {
			request := httptest.NewRequest(http.MethodGet, base+endpoint, nil)
			request.AddCookie(cookie)
			request.Header.Set("Range", seek.rangeHeader)
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, request)
			require.Equal(t, http.StatusPartialContent, w.Code)
			require.Equal(t, seek.expected, w.Body.String())
		}
		request := httptest.NewRequest(http.MethodGet, base+endpoint, nil)
		request.AddCookie(cookie)
		request.Header.Set("Range", "bytes=10-14,0-8")
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, request)
		require.Equal(t, http.StatusRequestedRangeNotSatisfiable, w.Code, "multipart ranges must not amplify archive decompression")
	}
	for _, url := range []string{base + "private.jpg", base + "../private.jpg", strings.Replace(base, "image-1", "gallery-4", 1) + "download", strings.Replace(base, "image-1", "image-2", 1) + "image"} {
		require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, url, cookie).Code)
	}
	require.NoError(t, os.WriteFile(zipPath, []byte("replaced archive"), 0o600))
	require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, base+"image", cookie).Code)
}

func TestSharedArchiveReadHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &sharedArchiveReader{ctx: ctx, size: 100}
	_, err := reader.Read(make([]byte, 1))
	require.ErrorIs(t, err, context.Canceled)
}

func TestSharingOriginalConfigurationIsExplicitAndPreservesOmittedOptions(t *testing.T) {
	c := config.InitializeEmpty()
	c.SetConfigFile(filepath.Join(t.TempDir(), "config.yml"))
	r := &mutationResolver{}
	enabled, disabled := true, false
	result, err := r.ConfigureSharing(context.Background(), "", &enabled, nil)
	require.NoError(t, err)
	require.False(t, result.ServeOriginalMedia, "preview reuse does not opt in to originals")
	result, err = r.ConfigureSharing(context.Background(), "", nil, &enabled)
	require.NoError(t, err)
	require.True(t, result.ServeOriginalMedia)
	data, err := os.ReadFile(c.GetConfigFile())
	require.NoError(t, err)
	require.Contains(t, string(data), "sharing_serve_original_media: true")
	result, err = r.ConfigureSharing(context.Background(), "", nil, nil)
	require.NoError(t, err)
	require.True(t, result.ServeOriginalMedia)
	_, err = r.ConfigureSharing(context.Background(), "invalid", nil, &disabled)
	require.Error(t, err)
	require.True(t, sharingConfiguration().ServeOriginalMedia)
	result, err = r.ConfigureSharing(context.Background(), "", nil, &disabled)
	require.NoError(t, err)
	require.False(t, result.ServeOriginalMedia)
	require.True(t, result.UseExistingPreviews)
}
