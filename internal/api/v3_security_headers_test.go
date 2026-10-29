package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stretchr/testify/require"
)

func TestPageSecurityHeadersAllowImageConversionAndOfflineWorker(t *testing.T) {
	cfg := config.NewEmpty()
	for _, path := range []string{"/", "/login", "/offline.html", "/images/123", "/performers/123"} {
		response := httptest.NewRecorder()
		setPageSecurityHeaders(cfg, response, httptest.NewRequest(http.MethodGet, path, nil), nil)
		directives := strings.Split(response.Header().Get("Content-Security-Policy"), ";")
		for i := range directives {
			directives[i] = strings.TrimSpace(directives[i])
		}
		// Copy image decodes fetched bytes through a blob URL before writing
		// PNG pixels. CSP's wildcard does not permit the blob scheme.
		require.Contains(t, directives, "img-src data: blob: *", path)
		require.Contains(t, directives, "worker-src blob: 'self'", path)
		require.Contains(t, directives, "child-src 'none'", path)
		require.Contains(t, directives, "object-src 'none'", path)
	}
}
