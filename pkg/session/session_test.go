package session

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type testConfig struct{}

func (testConfig) GetUsername() string        { return "owner" }
func (testConfig) GetAPIKey() string          { return "key" }
func (testConfig) GetSessionStoreKey() []byte { return []byte("01234567890123456789012345678901") }
func (testConfig) GetMaxSessionAge() int      { return 3600 }
func (testConfig) ValidateCredentials(user, pass string) bool {
	return user == "owner" && pass == "pass +&"
}

func TestCookieTransportAttributesOnLoginRefreshAndLogout(t *testing.T) {
	store := NewStore(testConfig{})
	for _, transport := range []string{"http", "https", "proxy"} {
		t.Run(transport, func(t *testing.T) {
			t.Parallel()
			request := func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "http://example.test/login", strings.NewReader("username=owner&password=pass+%2B%26"))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if transport == "https" {
					r.TLS = &tls.ConnectionState{}
				}
				if transport == "proxy" {
					r.Header.Set("X-Forwarded-Proto", "https")
				}
				return r
			}
			login := httptest.NewRecorder()
			require.NoError(t, store.Login(login, request()))
			cookies := login.Result().Cookies()
			require.Len(t, cookies, 1)
			for _, operation := range []string{"login", "refresh", "logout"} {
				cookie := cookies[0]
				if operation != "login" {
					r, w := request(), httptest.NewRecorder()
					r.AddCookie(cookies[0])
					if operation == "refresh" {
						user, err := store.GetSessionUserID(w, r)
						require.NoError(t, err)
						require.Equal(t, "owner", user)
					} else {
						require.NoError(t, store.Logout(w, r))
					}
					cookie = w.Result().Cookies()[0]
				}
				require.Equal(t, transport != "http", cookie.Secure, operation)
				require.True(t, cookie.HttpOnly, operation)
				require.Equal(t, http.SameSiteLaxMode, cookie.SameSite, operation)
			}
		})
	}
}
