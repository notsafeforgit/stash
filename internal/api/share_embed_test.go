package api

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/internal/sharing"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func linkCard(t *testing.T, handler http.Handler, target string) map[string]string {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Header.Set("User-Agent", "TelegramBot (like TwitterBot)")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Empty(t, w.Header().Get("Set-Cookie"))
	ret := map[string]string{}
	for _, match := range regexp.MustCompile(`<meta property="([^"]+)" content="([^"]*)"`).FindAllStringSubmatch(w.Body.String(), -1) {
		ret[match[1]] = html.UnescapeString(match[2])
	}
	return ret
}

func TestShareLinkImageCardWithoutCookies(t *testing.T) {
	rs, handler, row, secret := shareHTTPFixture(t)
	base := "https://shares.test/share/" + row.ID + "/"
	link := "https://shares.test/share/" + secret + "/"
	card := linkCard(t, handler, link)
	require.Equal(t, card, linkCard(t, handler, base+"?key="+secret), "existing links advertise the canonical URL without rotating")
	require.Equal(t, row.Label, card["og:title"])
	require.Equal(t, "summary_large_image", card["twitter:card"])
	require.Equal(t, link, card["og:url"])
	require.Empty(t, card["og:video"])
	require.Equal(t, link+"preview/image", card["og:image"])
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r := httptest.NewRequest(method, card["og:image"], nil)
		r.Header.Set("Origin", "https://web.telegram.org")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Range", "bytes=0-7")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusPartialContent, w.Code)
		require.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))
		require.Equal(t, "8", w.Header().Get("Content-Length"))
		require.Equal(t, "cross-origin", w.Header().Get("Cross-Origin-Resource-Policy"))
		require.Contains(t, w.Header().Get("Cache-Control"), "no-store")
		if method == http.MethodGet {
			require.Equal(t, "original", w.Body.String())
		} else {
			require.Empty(t, w.Body.String())
		}
	}
	bare := linkCard(t, handler, base)
	require.Empty(t, bare, "a share ID alone must not reveal metadata")
	other, otherSecret, err := rs.service.Create(t.Context(), "owner", sharing.Options{Label: "Other", ExpiresAt: time.Now().Add(time.Hour)}, []sharing.Target{{Kind: "IMAGE", ID: 1}})
	require.NoError(t, err)
	for _, invalid := range []string{"", "bad", otherSecret} {
		require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, base+"preview/image?key="+invalid, nil).Code)
	}
	require.NotEqual(t, row.ID, other.ID)
	for _, endpoint := range []string{"content", "status", "media/" + shareTestKey(t, rs, "image", 1) + "/image", "media/" + shareTestKey(t, rs, "image", 1) + "/download", "preview/video.mp4"} {
		w := sharedPreviewRequest(handler, http.MethodGet, base+endpoint+"?key="+secret, nil)
		require.Equal(t, http.StatusNotFound, w.Code, endpoint)
	}
	saved, err := rs.service.Find(t.Context(), row.ID)
	require.NoError(t, err)
	require.Zero(t, saved.AccessCount, "crawling does not count as a recipient visit")
	managed, err := shareResult(t.Context(), rs.service.Repo, saved)
	require.NoError(t, err)
	require.NotNil(t, managed.URL)
	// Management Preview and Copy link receive the same public, crawler-capable URL.
	parsed, err := url.Parse(*managed.URL)
	require.NoError(t, err)
	require.Equal(t, "/share/"+secret+"/", parsed.Path)
	require.Empty(t, parsed.RawQuery)
	require.Empty(t, parsed.Fragment)
	require.NoError(t, rs.service.Revoke(t.Context(), row.ID))
	for _, target := range []string{link, card["og:image"]} {
		require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, target, nil).Code)
	}
}

func TestShareLinkMetadataScopeAndRotation(t *testing.T) {
	rs, handler, _, _, db := shareHTTPMediaDatabaseFixture(t, false)
	title := `A "title" <script>alert(1)</script> & more`
	require.NoError(t, rs.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `UPDATE images SET title=? WHERE id=1`, []any{title})
		return err
	}))
	options := sharing.Options{Label: "Metadata scope", ExpiresAt: time.Now().Add(time.Hour), ShowMetadata: true}
	row, secret, err := rs.service.Create(t.Context(), "owner", options, []sharing.Target{{Kind: "IMAGE", ID: 1}})
	require.NoError(t, err)
	base := "https://shares.test/share/" + row.ID + "/"
	card := linkCard(t, handler, base+"?key="+secret)
	require.Equal(t, title, card["og:title"])
	w := sharedPreviewRequest(handler, http.MethodGet, base+"?key="+secret, nil)
	require.NotContains(t, w.Body.String(), "<script>alert(1)")
	require.NoError(t, rs.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO images(id,title,created_at,updated_at) VALUES(2,'Second image',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP); INSERT INTO images_files(image_id,file_id,"primary") VALUES(2,2,1);`, nil)
		return err
	}))
	rs.service.Repo.Image = db.Repository().Image
	multi, multiSecret, err := rs.service.Create(t.Context(), "owner", options, []sharing.Target{{Kind: "IMAGE", ID: 1}, {Kind: "IMAGE", ID: 2}})
	require.NoError(t, err)
	multiBase := "https://shares.test/share/" + multi.ID + "/"
	card = linkCard(t, handler, multiBase+"?key="+multiSecret)
	require.Equal(t, "summary", card["twitter:card"])
	require.Empty(t, card["og:image"])
	require.Equal(t, multi.Label, card["og:title"])
	require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, multiBase+"preview/image?key="+multiSecret, nil).Code)
	newSecret, err := rs.service.Rotate(t.Context(), row.ID)
	require.NoError(t, err)
	for _, endpoint := range []string{"", "preview/image"} {
		require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, base+endpoint+"?key="+secret, nil).Code)
	}
	card = linkCard(t, handler, base+"?key="+newSecret)
	require.Contains(t, card["og:image"], newSecret)
	saved, err := rs.service.Find(t.Context(), row.ID)
	require.NoError(t, err)
	saved.ExpiresAt = time.Now().Unix() - 1
	require.NoError(t, rs.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return rs.service.Repo.Share.Update(ctx, saved)
	}))
	require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, card["og:image"], nil).Code)
}

func TestShareLinkVideoCardAndFilePinning(t *testing.T) {
	rs, handler, row, secret := shareHTTPMediaFixture(t, true)
	snapshot, err := shareTestSnapshot(t, rs, row)
	require.NoError(t, err)
	f, _, err := rs.service.Resolve(t.Context(), snapshot.Media[0])
	require.NoError(t, err)
	f.Base().Basename = "private-source-name.mp4"
	base := "https://shares.test/share/" + row.ID + "/"
	card := linkCard(t, handler, base+"?key="+secret)
	require.Equal(t, "video.other", card["og:type"])
	require.Equal(t, "player", card["twitter:card"])
	require.Equal(t, "video/mp4", card["og:video:type"])
	require.Equal(t, "100", card["og:video:width"])
	require.Equal(t, card["og:video"], card["twitter:player:stream"])
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := sharedPreviewRequest(handler, method, card["og:video"], nil)
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "video/mp4", w.Header().Get("Content-Type"))
		require.Equal(t, "bytes", w.Header().Get("Accept-Ranges"))
		require.Contains(t, w.Header().Get("Content-Disposition"), "inline")
		require.NotContains(t, w.Header().Get("Content-Disposition"), "private-source")
	}
	f.Base().ID++
	require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, card["og:video"], nil).Code)
	require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, base+"?key="+secret, nil).Code)
}

func TestShareLinkPublicOriginAndMount(t *testing.T) {
	_, handler, row, secret := shareHTTPFixture(t)
	config.GetInstance().SetString(config.SharingPublicURL, "https://public.test/mounted/share")
	card := linkCard(t, handler, "http://internal.test/share/"+row.ID+"/?key="+secret)
	for _, key := range []string{"og:url", "og:image"} {
		require.True(t, strings.HasPrefix(card[key], "https://public.test/mounted/share/"), card[key])
	}
	ctx := context.WithValue(t.Context(), BaseURLCtxKey, "http://internal.test")
	link, err := url.Parse(shareLink(ctx, secret))
	require.NoError(t, err)
	require.Empty(t, link.Fragment)
	require.Equal(t, "/mounted/share/"+secret+"/", link.Path)
	require.Empty(t, link.RawQuery)
}

func TestShareShortLinkBootstrapAndCookieScope(t *testing.T) {
	rs, handler, row, secret := shareHTTPFixture(t)
	base := "https://shares.test/share/" + secret + "/"
	cookie := exchangeShareHTTP(t, handler, row, secret)
	for _, suffix := range []string{"", "?tab=images&media=" + shareTestKey(t, rs, "image", 1)} {
		w := sharedPreviewRequest(handler, http.MethodGet, base+suffix, nil)
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), `<meta name="stash-share-id" content="`+row.ID+`"/>`)
		require.NotContains(t, w.Body.String(), cookie.Value)
		require.Empty(t, w.Header().Get("Set-Cookie"))
		head := sharedPreviewRequest(handler, http.MethodHead, base+suffix, nil)
		require.Equal(t, http.StatusOK, head.Code)
		require.Empty(t, head.Body.String())
		require.Equal(t, w.Header().Get("Content-Length"), head.Header().Get("Content-Length"))
	}
	for _, endpoint := range []string{"content", "status", "media/" + shareTestKey(t, rs, "image", 1) + "/image"} {
		r := httptest.NewRequest(http.MethodGet, base+endpoint, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusNotFound, w.Code, "capability paths are not browser API paths")
	}
	for _, invalid := range []string{strings.Repeat("A", 43), secret + "=", secret + "%0A", secret[:42]} {
		w := sharedPreviewRequest(handler, http.MethodGet, "/share/"+invalid+"/", nil)
		require.Equal(t, http.StatusNotFound, w.Code)
		require.NotContains(t, w.Body.String(), row.ID)
	}
	rotated, err := rs.service.Rotate(t.Context(), row.ID)
	require.NoError(t, err)
	for _, endpoint := range []string{"", "preview/image"} {
		require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, base+endpoint, nil).Code)
	}
	card := linkCard(t, handler, "/share/"+rotated+"/")
	require.Equal(t, row.Label, card["og:title"])
	require.NoError(t, rs.service.Revoke(t.Context(), row.ID))
	require.Equal(t, http.StatusNotFound, sharedPreviewRequest(handler, http.MethodGet, "/share/"+rotated+"/", nil).Code)
}

func TestShareLinkVideoCompatibility(t *testing.T) {
	vf := &models.VideoFile{BaseFile: &models.BaseFile{Basename: "video.mp4"}, VideoCodec: "h264", AudioCodec: "aac"}
	require.True(t, directLinkVideo(vf))
	for _, codec := range []string{"av1", "hevc", "vp9"} {
		vf.VideoCodec = codec
		require.False(t, directLinkVideo(vf))
	}
	vf.VideoCodec, vf.AudioCodec = "h264", "opus"
	require.False(t, directLinkVideo(vf))
	vf.AudioCodec = "aac"
	depth := 10
	vf.BitDepth = &depth
	require.False(t, directLinkVideo(vf))
	vf.BitDepth = nil
	hlg := "arib-std-b67"
	vf.ColorTransfer = &hlg
	require.False(t, directLinkVideo(vf))
	vf.ColorTransfer = nil
	vf.Basename = "video.mkv"
	require.False(t, directLinkVideo(vf))
}
