package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/session"
	"github.com/stretchr/testify/require"
)

func TestNativeLoginPageAndLocale(t *testing.T) {
	t.Parallel()
	c := config.NewEmpty()
	c.SetString(config.Username, "test")
	c.SetPassword("test password")
	r := httptest.NewRequest(http.MethodGet, "https://library.test/login?returnURL=%2Fmounted%2Fscenes", nil)
	r.Header.Set("X-Forwarded-Prefix", "/mounted")
	w := httptest.NewRecorder()
	handleLogin(c).ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `<base href="/mounted/"`)
	require.Contains(t, w.Body.String(), `src="login/assets/login-`)
	require.NotContains(t, w.Body.String(), "XMLHttpRequest")
	require.NotContains(t, w.Body.String(), "<input")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	c.SetString(config.Language, "missing-locale")
	w = httptest.NewRecorder()
	handleLoginLocale(c).ServeHTTP(w, r)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	var locale struct {
		Locale   string
		Messages map[string]string
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &locale))
	require.Equal(t, "en-GB", locale.Locale)
	require.Equal(t, "Username", locale.Messages["login.username"])
}

func TestLoginReturnURLStaysWithinMount(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "https://library.test/login", nil)
	r.Header.Set("X-Forwarded-Prefix", "/mounted")
	for _, unsafe := range []string{"", "//outside.test/", "https://outside.test/mounted/", "https://user@library.test/mounted/", "/mounted/../outside", "/mounted-other/", "/mounted/login", "javascript:alert(1)", "/mounted/%5coutside"} {
		require.Equal(t, "/mounted/", safeLoginReturnURL(r, unsafe), unsafe)
	}
	require.Equal(t, "/mounted/scenes?filter=a%26b#selection", safeLoginReturnURL(r, "https://library.test/mounted/scenes?filter=a%26b#selection"))
}

func TestLoginPostUsesInjectedStoreAndSetsUsableSession(t *testing.T) {
	t.Parallel()
	c := config.NewEmpty()
	c.SetString(config.Username, "test")
	c.SetPassword("test password")
	c.SetString(config.SessionStoreKey, strings.Repeat("k", 32))
	c.SetInt(config.MaxSessionAge, 3600)
	store := session.NewStore(c)
	for _, password := range []string{"wrong", "test password"} {
		form := url.Values{"username": {"test"}, "password": {password}}
		r := httptest.NewRequest(http.MethodPost, "https://library.test/login", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handleLoginPost(store).ServeHTTP(w, r)
		if password == "wrong" {
			require.Equal(t, http.StatusUnauthorized, w.Code)
			require.Empty(t, w.Result().Cookies())
			continue
		}
		require.Equal(t, http.StatusOK, w.Code)
		request := httptest.NewRequest(http.MethodPost, "/graphql", nil)
		for _, cookie := range w.Result().Cookies() {
			request.AddCookie(cookie)
		}
		user, err := store.Authenticate(httptest.NewRecorder(), request)
		require.NoError(t, err)
		require.Equal(t, "test", user)
	}
}
