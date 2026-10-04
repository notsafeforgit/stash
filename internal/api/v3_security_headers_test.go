package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stretchr/testify/require"
)

func TestPageSecurityHeadersAllowOfflineWorkerByDefault(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	for _, path := range []string{"/", "/offline.html", "/performers/123"} {
		response := httptest.NewRecorder()
		setPageSecurityHeaders(response, httptest.NewRequest(http.MethodGet, path, nil), nil)
		directives := strings.Split(response.Header().Get("Content-Security-Policy"), ";")
		for i := range directives {
			directives[i] = strings.TrimSpace(directives[i])
		}
		require.Contains(t, directives, "worker-src blob: 'self'", path)
		require.Contains(t, directives, "child-src 'none'", path)
		require.Contains(t, directives, "object-src 'none'", path)
	}
}
