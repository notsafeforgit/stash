package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stretchr/testify/require"
)

type settingsRuntimeRecorder struct {
	settingsRuntime // unexpected refreshes fail rather than silently succeeding
	refreshes       int
}

func (s *settingsRuntimeRecorder) RefreshConfig()       { s.refreshes++ }
func (s *settingsRuntimeRecorder) RefreshScraperCache() { s.refreshes++ }

func newSettingsResolver(t *testing.T) (*mutationResolver, *config.Config, *settingsRuntimeRecorder) {
	t.Helper()
	c := config.NewEmpty()
	c.SetConfigFile(filepath.Join(t.TempDir(), "config.yml"))
	c.SetString(config.Username, "original")
	c.SetString(config.ApiKey, "original key")
	require.NoError(t, c.Write())
	runtime := &settingsRuntimeRecorder{}
	return &mutationResolver{&Resolver{settings: &settingsService{config: c, runtime: runtime}}}, c, runtime
}

func TestSettingsLateValidationDoesNotPublishOrRefresh(t *testing.T) {
	t.Parallel()
	r, c, runtime := newSettingsResolver(t)
	before, err := os.ReadFile(c.GetConfigFile())
	require.NoError(t, err)
	username, invalidRegex, userAgent := "unsaved", "[", "unsaved agent"
	_, err = r.ConfigureGeneral(t.Context(), ConfigGeneralInput{Username: &username, Excludes: []string{invalidRegex}})
	require.ErrorContains(t, err, "video exclusion pattern")
	_, err = r.ConfigureScraping(t.Context(), ConfigScrapingInput{ScraperUserAgent: &userAgent, ExcludeTagPatterns: []string{invalidRegex}})
	require.ErrorContains(t, err, "tag exclusion pattern")
	require.Equal(t, "original", c.GetUsername())
	require.NotEqual(t, userAgent, c.GetScraperUserAgent())
	require.Zero(t, runtime.refreshes)
	after, err := os.ReadFile(c.GetConfigFile())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestSettingsWriteFailurePreservesAllSections(t *testing.T) {
	t.Parallel()
	for _, section := range []string{"general", "interface", "dlna", "scraping", "defaults", "plugin", "api key", "sharing", "enabled plugins", "plugin hook order"} {
		t.Run(section, func(t *testing.T) {
			r, c, runtime := newSettingsResolver(t)
			c.SetConfigFile(filepath.Join(t.TempDir(), "missing", "config.yml"))
			before, err := c.Marshal()
			require.NoError(t, err)
			changed, yes := "changed", true
			switch section {
			case "general":
				_, err = r.ConfigureGeneral(t.Context(), ConfigGeneralInput{Username: &changed})
			case "interface":
				_, err = r.ConfigureInterface(t.Context(), ConfigInterfaceInput{CSS: &changed, CSSEnabled: &yes})
			case "dlna":
				_, err = r.ConfigureDlna(t.Context(), ConfigDLNAInput{ServerName: &changed, Enabled: &yes})
			case "scraping":
				_, err = r.ConfigureScraping(t.Context(), ConfigScrapingInput{ScraperUserAgent: &changed})
			case "defaults":
				_, err = r.ConfigureDefaults(t.Context(), ConfigDefaultSettingsInput{DeleteFile: &yes})
			case "plugin":
				_, err = r.ConfigurePlugin(t.Context(), "fixture", map[string]interface{}{"changed": true})
			case "sharing":
				_, err = r.ConfigureSharing(t.Context(), "https://example.test/share")
			case "enabled plugins":
				_, err = r.SetPluginsEnabled(t.Context(), map[string]bool{"fixture": false})
			case "plugin hook order":
				_, err = r.SetPluginHookOrder(t.Context(), PluginHookOrderInput{Hook: "Scene.Update.Post", PluginIds: []string{"fixture"}})
			case "api key":
				var key string
				key, err = r.GenerateAPIKey(t.Context(), GenerateAPIKeyInput{Clear: &yes})
				require.Empty(t, key)
			}
			require.Error(t, err)
			after, err := c.Marshal()
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Zero(t, runtime.refreshes)
		})
	}
}

func TestSettingsConcurrentSavesRetainBothSections(t *testing.T) {
	t.Parallel()
	r, c, runtime := newSettingsResolver(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Go(func() {
		name := "saved user"
		_, err := r.ConfigureGeneral(t.Context(), ConfigGeneralInput{Username: &name})
		errs <- err
	})
	wg.Go(func() {
		agent := "saved agent"
		_, err := r.ConfigureScraping(t.Context(), ConfigScrapingInput{ScraperUserAgent: &agent})
		errs <- err
	})
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, "saved user", c.GetUsername())
	require.Equal(t, "saved agent", c.GetScraperUserAgent())
	require.Equal(t, 2, runtime.refreshes)
	data, err := os.ReadFile(c.GetConfigFile())
	require.NoError(t, err)
	require.Contains(t, string(data), "saved user")
	require.Contains(t, string(data), "saved agent")
}

func TestCustomizationHandlersReadAtomicSavedValues(t *testing.T) {
	t.Parallel()
	r, c, _ := newSettingsResolver(t)
	css, javascript, locales, enabled := "body {color: red;}", "void 0;", `{"en-GB":{"example":"Saved"}}`, true
	_, err := r.ConfigureInterface(context.Background(), ConfigInterfaceInput{
		CSS: &css, CSSEnabled: &enabled, Javascript: &javascript, JavascriptEnabled: &enabled,
		CustomLocales: &locales, CustomLocalesEnabled: &enabled,
	})
	require.NoError(t, err)
	for _, tt := range []struct {
		handler http.HandlerFunc
		want    string
	}{
		{cssHandler(c), css}, {javascriptHandler(c), javascript}, {customLocalesHandler(c), locales},
	} {
		w := httptest.NewRecorder()
		tt.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/customization", nil))
		require.Equal(t, tt.want, w.Body.String())
	}
}

func TestCredentialsAreWriteOnlyAndExplicit(t *testing.T) {
	t.Parallel()
	r, c, _ := newSettingsResolver(t)
	password, renamed := "first password", "renamed"
	result, err := r.ConfigureGeneral(t.Context(), ConfigGeneralInput{NewPassword: &password})
	require.NoError(t, err)
	require.True(t, result.PasswordConfigured)
	hash := c.GetPasswordHash()
	data, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(data), hash)
	require.NotContains(t, string(data), password)
	_, err = r.ConfigureGeneral(t.Context(), ConfigGeneralInput{Username: &renamed})
	require.NoError(t, err)
	require.Equal(t, hash, c.GetPasswordHash(), "omitting newPassword retains the password")
	require.True(t, c.ValidateCredentials(renamed, password))

	// A hash-shaped password is ordinary new plaintext, never an unchanged
	// sentinel. Returning hashes was both unnecessary disclosure and ambiguity.
	_, err = r.ConfigureGeneral(t.Context(), ConfigGeneralInput{NewPassword: &hash})
	require.NoError(t, err)
	require.NotEqual(t, hash, c.GetPasswordHash())
	require.True(t, c.ValidateCredentials(renamed, hash))
	for _, invalid := range []string{"", strings.Repeat("a", 73), strings.Repeat("é", 37)} {
		_, err = r.ConfigureGeneral(t.Context(), ConfigGeneralInput{NewPassword: &invalid})
		require.ErrorContains(t, err, "1 to 72 UTF-8 bytes")
		require.True(t, c.ValidateCredentials(renamed, hash))
	}
	yes := true
	_, err = r.ConfigureGeneral(t.Context(), ConfigGeneralInput{NewPassword: &password, ClearPassword: &yes})
	require.ErrorContains(t, err, "cannot be used together")
	require.True(t, c.ValidateCredentials(renamed, hash))
	result, err = r.ConfigureGeneral(t.Context(), ConfigGeneralInput{ClearPassword: &yes})
	require.NoError(t, err)
	require.False(t, result.PasswordConfigured)
	require.False(t, c.HasCredentials())
}
