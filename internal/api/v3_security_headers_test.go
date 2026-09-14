package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stretchr/testify/require"
)

func TestV3PageSecurityHeadersAllowOfflineWorker(t *testing.T) {
	for _, tt := range []struct {
		name       string
		enabled    bool
		workerRule string
	}{
		{name: "v2", workerRule: "worker-src blob:"},
		{name: "v3", enabled: true, workerRule: "worker-src blob: 'self'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.InitializeEmpty()
			t.Cleanup(func() { config.InitializeEmpty() })
			cfg.SetBool(config.EnableV3UI, tt.enabled)
			for _, path := range []string{"/", "/offline.html", "/performers/123"} {
				response := httptest.NewRecorder()
				setPageSecurityHeaders(response, httptest.NewRequest(http.MethodGet, path, nil), nil)
				directives := strings.Split(response.Header().Get("Content-Security-Policy"), ";")
				for i := range directives {
					directives[i] = strings.TrimSpace(directives[i])
				}
				require.Contains(t, directives, tt.workerRule, path)
				require.Contains(t, directives, "child-src 'none'", path)
				require.Contains(t, directives, "object-src 'none'", path)
			}
		})
	}
}
