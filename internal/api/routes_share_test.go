package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/internal/sharing"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stashapp/stash/pkg/sqlite"
	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func shareHTTPFixture(t *testing.T) (*shareRoutes, http.Handler, *models.ShareRecord, string) {
	t.Helper()
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "share.sqlite")))
	t.Cleanup(func() { _ = db.Close() })
	repo := db.Repository()
	m := mocks.NewDatabase()
	repo.Image = m.Image
	filePath := filepath.Join(t.TempDir(), "secret-image-name.jpg")
	require.NoError(t, os.WriteFile(filePath, []byte("original image bytes"), 0o600))
	f := &models.ImageFile{BaseFile: &models.BaseFile{ID: 2, Path: filePath, Basename: "secret-image-name.jpg"}, Width: 100, Height: 80}
	m.Image.On("Find", mock.Anything, 1).Return(&models.Image{ID: 1, Title: "Private title", Files: models.NewRelatedFiles([]models.File{f})}, nil)
	service := sharing.New(repo)
	row, secret, err := service.Create(context.Background(), "owner", sharing.Options{Label: "Example share", ExpiresAt: time.Now().Add(time.Hour)}, []sharing.Target{{Kind: "IMAGE", ID: 1}})
	require.NoError(t, err)
	rs := &shareRoutes{service: service, budget: newShareBudget()}
	return rs, rs.router(), row, secret
}

func exchangeShareHTTP(t *testing.T, handler http.Handler, row *models.ShareRecord, secret string) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "https://shares.test/share/"+row.ID+"/exchange", strings.NewReader(fmt.Sprintf(`{"secret":%q}`, secret)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://shares.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	require.Len(t, response.Result().Cookies(), 1)
	cookie := response.Result().Cookies()[0]
	require.True(t, cookie.Secure)
	require.True(t, cookie.HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	require.Equal(t, "/share/"+row.ID+"/", cookie.Path)
	return cookie
}

func TestShareHTTPScopesEveryMediaRoute(t *testing.T) {
	rs, handler, row, secret := shareHTTPFixture(t)
	base := "/share/" + row.ID + "/"
	cookie := exchangeShareHTTP(t, handler, row, secret)
	for _, endpoint := range []string{
		"content", "status", "media/image-1/", "media/image-1/image", "media/image-1/thumbnail", "media/image-1/download",
		"media/image-1/stream.master.m3u8", "media/image-1/stream.m3u8/video.m3u8", "media/image-1/stream.m3u8/video/init.mp4", "media/image-1/stream.m3u8/video/0.m4s",
		"media/image-1/stream.fmp4.master.m3u8", "media/image-1/stream.fmp4.m3u8/audio/init.mp4", "media/image-1/stream.fmp4.aac.m3u8/audio/0.m4s",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			request := httptest.NewRequest(method, base+endpoint+"?apikey=owner-key&token="+secret, nil)
			request.Header.Set("ApiKey", "owner-key")
			request.Header.Set("Authorization", "Bearer "+secret)
			request.AddCookie(&http.Cookie{Name: "session", Value: "owner-session"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Contains(t, []int{404, 405}, response.Code, endpoint)
			require.Contains(t, response.Header().Get("Cache-Control"), "no-store")
		}
	}
	get := func(endpoint string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, base+endpoint, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	response := get("content")
	require.Equal(t, http.StatusOK, response.Code)
	for _, private := range []string{"Private title", "secret-image-name", "apikey", "fingerprint", "file_id", "created_by", secret, cookie.Value} {
		require.NotContains(t, response.Body.String(), private)
	}
	require.Empty(t, response.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))
	var content publicShareContent
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &content))
	require.Len(t, content.Media, 1)
	require.Equal(t, base+"media/image-1/image", content.Media[0].Image)
	for _, endpoint := range []string{"media/image-2/", "media/image-2/image", "media/image-1/download", "graphql", "scene/1/stream", "../graphql", "media/image-1/%2e%2e/download", "media//image-1/", "media/image-1/../../graphql"} {
		require.Equal(t, http.StatusNotFound, get(endpoint).Code, endpoint)
	}
	_, err := rs.service.Update(context.Background(), row.ID, sharing.Options{Label: row.Label, ExpiresAt: time.Unix(row.ExpiresAt, 0), AllowDownload: true})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, base+"media/image-1/download", nil)
	request.AddCookie(cookie)
	request.Header.Set("Range", "bytes=0-7")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusPartialContent, response.Code)
	require.Equal(t, "original", response.Body.String())
	require.NotContains(t, response.Header().Get("Content-Disposition"), "secret-image-name")
	require.NoError(t, rs.service.Revoke(context.Background(), row.ID))
	for _, endpoint := range []string{"content", "status", "media/image-1/", "media/image-1/download"} {
		require.Equal(t, http.StatusNotFound, get(endpoint).Code)
	}
}

func TestShareHTTPExchangeRejectsCrossOriginAndWrongSecrets(t *testing.T) {
	_, handler, row, secret := shareHTTPFixture(t)
	for _, input := range []struct{ origin, site, contentType, body string }{
		{"https://evil.test", "cross-site", "application/json", fmt.Sprintf(`{"secret":%q}`, secret)},
		{"https://sibling.shares.test", "same-site", "application/json", fmt.Sprintf(`{"secret":%q}`, secret)},
		{"", "", "application/json", fmt.Sprintf(`{"secret":%q}`, secret)},
		{"https://shares.test", "same-origin", "text/plain", fmt.Sprintf(`{"secret":%q}`, secret)},
		{"https://shares.test", "same-origin", "application/json", `{"secret":"bad"}`},
		{"https://shares.test", "same-origin", "application/json", fmt.Sprintf(`{"secret":%q} {}`, secret)},
		{"https://shares.test", "same-origin", "application/json", fmt.Sprintf(`{"secret":%q,"id":"other"}`, secret)},
	} {
		request := httptest.NewRequest(http.MethodPost, "https://shares.test/share/"+row.ID+"/exchange", strings.NewReader(input.body))
		request.Header.Set("Origin", input.origin)
		request.Header.Set("Sec-Fetch-Site", input.site)
		request.Header.Set("Content-Type", input.contentType)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusNotFound, response.Code)
		require.Empty(t, response.Header().Get("Set-Cookie"))
	}
	cookie := exchangeShareHTTP(t, handler, row, secret)
	otherID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))
	request := httptest.NewRequest(http.MethodGet, "/share/"+otherID+"/content", nil)
	request.AddCookie(&http.Cookie{Name: shareCookieName(otherID), Value: cookie.Value})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestShareResponseCancelsAndRedacts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	recorder := httptest.NewRecorder()
	response := &shareResponse{ResponseWriter: recorder, ctx: ctx, budget: newShareBudget(), shareID: "test"}
	response.Header().Set("Cache-Control", "public, max-age=3600")
	response.WriteHeader(http.StatusInternalServerError)
	_, err := response.Write([]byte("ffmpeg failed reading /private/library/video.mp4"))
	require.NoError(t, err)
	require.NotContains(t, recorder.Body.String(), "/private")
	require.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
	cancel()
	response = &shareResponse{ResponseWriter: httptest.NewRecorder(), ctx: ctx, budget: newShareBudget(), shareID: "test"}
	n, err := response.Write([]byte("bytes after revoke"))
	require.Zero(t, n)
	require.ErrorIs(t, err, context.Canceled)
}

func TestShareShellAndAssetsHaveNoApplicationFallback(t *testing.T) {
	_, handler, row, _ := shareHTTPFixture(t)
	request := httptest.NewRequest(http.MethodGet, "/share/"+row.ID+"/", nil)
	request.Header.Set("X-Forwarded-Prefix", "/mounted")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "1", response.Header().Get("X-Stash-Share-Version"))
	require.Contains(t, response.Body.String(), `<base href="/mounted/share/"`)
	require.NotContains(t, response.Body.String(), `src="javascript"`)
	require.NotContains(t, response.Body.String(), `service-worker`)
	assets := regexp.MustCompile(`(?:src|href)="\./assets/([^"]+)"`).FindAllStringSubmatch(response.Body.String(), -1)
	require.NotEmpty(t, assets, "build the v3 share entrypoint before testing")
	for _, asset := range assets {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/share/assets/"+asset[1], nil))
		require.Equal(t, http.StatusOK, response.Code, asset[1])
		require.NotContains(t, response.Header().Get("Content-Type"), "text/html")
	}
	for _, path := range []string{"/share/assets", "/share/assets/", "/share/assets/missing.js", "/share/assets/file.js.map", "/share/graphql", "/share/../graphql", "/share/assets/%2e%2e/index.html"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusNotFound, response.Code, path)
	}
}

func TestShareEncoderBudgetAndExpiry(t *testing.T) {
	b := newShareBudget()
	defer b.releaseShare("share")
	for i := range 3 {
		require.True(t, b.stream("share", fmt.Sprint(i), 1, fmt.Sprint(i), time.Now().Add(time.Hour)))
	}
	require.False(t, b.stream("share", "fourth", 1, "fourth", time.Now().Add(time.Hour)))
	b.releaseShare("share")
	stopped := make(chan string, 1)
	b.stopStream = func(_ models.FileID, session string) { stopped <- session }
	require.True(t, b.stream("share", "short", 1, "short", time.Now().Add(20*time.Millisecond)))
	select {
	case session := <-stopped:
		require.Equal(t, "short", session)
	case <-time.After(time.Second):
		t.Fatal("expired encoder was not stopped")
	}
}
