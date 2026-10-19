package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/session"
	"github.com/stashapp/stash/pkg/utils"
	"github.com/stashapp/stash/ui"
)

const (
	returnURLParam = "returnURL"

	defaultLocale = "en-GB"
)

func getLoginPage() []byte {
	data, err := fs.ReadFile(ui.UIBox, "login.html")
	if err != nil {
		panic(err)
	}
	return data
}

func serveLoginPage(cfg *config.Config, w http.ResponseWriter, r *http.Request) {
	loginPage := string(getLoginPage())
	prefix := getProxyPrefix(r)
	loginPage = strings.Replace(loginPage, `<base href="/"`, fmt.Sprintf(`<base href="%s/"`, html.EscapeString(prefix)), 1)
	// Keep the built-in login available with custom UIs and during upgrades.
	// Its shared chunks load beneath this dedicated unauthenticated asset path.
	loginPage = strings.ReplaceAll(loginPage, `"./assets/`, `"login/assets/`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	setPageSecurityHeaders(cfg, w, r, nil)
	utils.ServeStaticContent(w, r, []byte(loginPage))
}

// Login return links are limited to this origin and application mount.
func safeLoginReturnURL(r *http.Request, value string) string {
	prefix := getProxyPrefix(r)
	fallback := prefix + "/"
	u, err := url.Parse(value)
	if err != nil || u.User != nil || strings.Contains(value, `\`) || strings.Contains(u.Path, `\`) {
		return fallback
	}
	if u.IsAbs() && ((u.Scheme != "https" && u.Scheme != "http") || u.Host != r.Host) {
		return fallback
	}
	if u.Host != "" && !u.IsAbs() {
		return fallback
	}
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return fallback
	}
	u.Path = path.Clean(u.Path)
	u.RawPath = ""
	if !strings.HasPrefix(u.Path, fallback) || u.Path == prefix+loginEndpoint {
		return fallback
	}
	u.Scheme, u.Host = "", ""
	return u.String()
}

func handleLoginLocale(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// get the locale from the config
		lang := cfg.GetLanguage()
		if lang == "" {
			lang = defaultLocale
		}

		data, err := getLoginLocale(lang)
		if err != nil {
			logger.Debugf("Failed to load login locale file for language %s: %v", lang, err)
			// try again with the default language
			if lang != defaultLocale {
				lang = defaultLocale
				data, err = getLoginLocale(defaultLocale)
				if err != nil {
					logger.Errorf("Failed to load login locale file for default language %s: %v", defaultLocale, err)
				}
			}

			// if there's still an error, response with an internal server error
			if err != nil {
				http.Error(w, "Failed to load login locale file", http.StatusInternalServerError)
				return
			}
		}

		var raw map[string]string
		if err := json.Unmarshal(data, &raw); err != nil {
			http.Error(w, "Invalid login locale", http.StatusInternalServerError)
			return
		}
		messages := make(map[string]string, len(raw))
		for key, value := range raw {
			messages["login."+key] = value
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			Locale   string            `json:"locale"`
			Messages map[string]string `json:"messages"`
		}{lang, messages})
	}
}

func getLoginLocale(lang string) ([]byte, error) {
	data, err := fs.ReadFile(ui.LoginUIBox, "locales/"+lang+".json")
	if err != nil {
		return nil, err
	}
	return data, nil
}

func handleLogin(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		returnURL := safeLoginReturnURL(r, r.URL.Query().Get(returnURLParam))

		if !cfg.HasCredentials() {
			http.Redirect(w, r, returnURL, http.StatusFound)
			return
		}

		serveLoginPage(cfg, w, r)
	}
}

func handleLoginPost(store *session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := store.Login(w, r)
		if err != nil {
			// always log the error
			logger.Errorf("Error logging in: %v from IP: %s", err, r.RemoteAddr)
		}

		var invalidCredentialsError *session.InvalidCredentialsError

		if errors.As(err, &invalidCredentialsError) {
			http.Error(w, "Username or password is invalid", http.StatusUnauthorized)
			return
		}

		if err != nil {
			// don't expose the error to the user
			http.Error(w, "An unexpected error occurred. See logs", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func handleLogout(cfg *config.Config, store *session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := store.Logout(w, r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// redirect to the login page if credentials are required
		prefix := getProxyPrefix(r)
		if cfg.HasCredentials() {
			http.Redirect(w, r, prefix+loginEndpoint, http.StatusFound)
		} else {
			http.Redirect(w, r, prefix+"/", http.StatusFound)
		}
	}
}
