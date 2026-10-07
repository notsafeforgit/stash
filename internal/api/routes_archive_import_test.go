package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestArchiveImportHTTPReadOnlyAndStrictFilters(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "imports.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	handler := (&nativeArchiveRoutes{repo: db.Repository()}).router()
	for _, kind := range []string{"catalog", "automation"} {
		path := "/import-history/" + kind
		for _, tc := range []struct {
			method, suffix string
			status         int
		}{
			{http.MethodGet, "", 200}, {http.MethodGet, "?limit=1", 200},
			{http.MethodGet, "?after=" + uuid.NewString(), 200},
			{http.MethodGet, "/" + uuid.NewString(), 404},
			{http.MethodGet, "?after=bad", 400}, {http.MethodGet, "?after=", 400},
			{http.MethodGet, "?limit=0", 400}, {http.MethodGet, "?limit=101", 400},
			{http.MethodGet, "?limit=one", 400}, {http.MethodGet, "?limit=1&limit=2", 400},
			{http.MethodGet, "?state=review", 400}, {http.MethodGet, "/bad", 400},
			{http.MethodGet, "/" + uuid.NewString() + "?activate=true", 400},
			{http.MethodPost, "", 405}, {http.MethodPut, "/" + uuid.NewString(), 405},
		} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(tc.method, path+tc.suffix, nil))
			require.Equal(t, tc.status, w.Code, "%s %s: %s", tc.method, path+tc.suffix, w.Body.String())
			switch tc.status {
			case 200:
				require.JSONEq(t, "[]", w.Body.String())
			case 400:
				require.Contains(t, w.Body.String(), "invalid_import_history_request")
			}
		}
	}
	for _, path := range []string{"/import-history/unknown", "/import-history/unknown/" + uuid.NewString()} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
}
