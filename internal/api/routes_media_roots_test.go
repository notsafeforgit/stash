package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeMediaRootReviewAndHistory(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "roots.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	handler := (&nativeArchiveRoutes{repo: db.Repository()}).router()
	request := func(method, target string, input interface{}) *httptest.ResponseRecorder {
		body, err := json.Marshal(input)
		require.NoError(t, err)
		r := httptest.NewRequest(method, target, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	dir := filepath.Join(t.TempDir(), "media")
	require.NoError(t, os.Mkdir(dir, 0700))
	probe := func(path string) models.MediaRootBinding {
		w := request(http.MethodPost, "/media-roots/probe", map[string]string{"server_path": path})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var binding models.MediaRootBinding
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &binding))
		return binding
	}
	binding := probe(dir + string(os.PathSeparator) + ".")
	require.Equal(t, dir, binding.Path)
	require.NotEmpty(t, binding.DirectoryIdentity)
	w := request(http.MethodGet, "/media-roots", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, "[]", w.Body.String(), "probing cannot register a root")
	id := "10000000-0000-4000-8000-000000000098"
	input := map[string]interface{}{"uuid": id, "expected_revision": 0, "label": "Media", "state": "active", "binding": binding, "reason": "Initial mount"}
	// Replace the directory after the user checked it: saving that old identity
	// must fail without creating a partial logical root.
	require.NoError(t, os.Rename(dir, dir+"-original"))
	require.NoError(t, os.Mkdir(dir, 0700))
	w = request(http.MethodPut, "/media-roots/"+id, input)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_root_binding")
	require.Equal(t, http.StatusNotFound, request(http.MethodGet, "/media-roots/"+id, nil).Code)
	binding = probe(dir)
	input["binding"] = binding
	w = request(http.MethodPut, "/media-roots/"+id, input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var first models.MediaRoot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))
	require.Equal(t, id, first.UUID)
	require.Equal(t, 1, first.Revision)
	require.Equal(t, binding, *first.Binding)
	require.Equal(t, http.StatusConflict, request(http.MethodPut, "/media-roots/"+id, input).Code)
	input["expected_revision"] = 1
	w = request(http.MethodPut, "/media-roots/"+id, input)
	require.Equal(t, http.StatusOK, w.Code)
	var unchanged models.MediaRoot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &unchanged))
	require.Equal(t, first, unchanged)
	// Renaming or disabling an offline root keeps the saved binding. Reactivation
	// is different: it must verify the actual directory again.
	require.NoError(t, os.Rename(dir, dir+"-moved"))
	input["state"], input["label"], input["reason"] = "disabled", "Offline media", "Disable unavailable mount"
	w = request(http.MethodPut, "/media-roots/"+id, input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	input["expected_revision"], input["state"] = 2, "active"
	require.Equal(t, http.StatusBadRequest, request(http.MethodPut, "/media-roots/"+id, input).Code)
	rebound := probe(dir + "-moved")
	require.Equal(t, binding.DirectoryIdentity, rebound.DirectoryIdentity)
	input["binding"], input["reason"] = rebound, "Reviewed moved directory"
	w = request(http.MethodPut, "/media-roots/"+id, input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = request(http.MethodGet, "/media-roots/"+id+"/history?limit=2", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var rows []models.MediaRootRevision
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rows))
	require.Len(t, rows, 2)
	require.Equal(t, *first.Binding, *rows[0].Binding)
	require.Equal(t, "Initial mount", rows[0].Reason)
	w = request(http.MethodGet, "/media-roots/"+id+"/history?after=2&limit=2", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rows))
	require.Len(t, rows, 1)
	require.Equal(t, rebound, *rows[0].Binding)
	require.Equal(t, 3, rows[0].Revision)
	input["expected_revision"], input["state"] = 3, "retired"
	require.Equal(t, http.StatusOK, request(http.MethodPut, "/media-roots/"+id, input).Code)
	input["expected_revision"], input["state"] = 4, "active"
	require.Equal(t, http.StatusConflict, request(http.MethodPut, "/media-roots/"+id, input).Code)
	for _, target := range []string{
		"/media-roots/" + id + "/history?after=-1", "/media-roots/" + id + "/history?limit=101", "/media-roots/bad/history",
	} {
		require.Equal(t, http.StatusBadRequest, request(http.MethodGet, target, nil).Code, target)
	}
	require.Equal(t, http.StatusNotFound, request(http.MethodGet, "/media-roots/10000000-0000-4000-8000-000000000097/history", nil).Code)
	for _, path := range []string{"relative", dir, dir + "\x00", ""} {
		require.Equal(t, http.StatusBadRequest, request(http.MethodPost, "/media-roots/probe", map[string]string{"server_path": path}).Code)
	}
	// Saves require the checked binding, not a path to be silently reprobed.
	require.Equal(t, http.StatusBadRequest, request(http.MethodPost, "/media-roots", map[string]interface{}{"label": "Old input", "state": "active", "server_path": dir + "-moved"}).Code)
	input["uuid"] = "10000000-0000-4000-8000-000000000097"
	require.Equal(t, http.StatusBadRequest, request(http.MethodPut, "/media-roots/"+id, input).Code)
	require.Equal(t, http.StatusBadRequest, request(http.MethodPut, "/media-roots/bad", input).Code)
	requestBody := bytes.NewBufferString(`{"server_path":"` + filepath.ToSlash(dir) + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/media-roots/probe", requestBody)
	req.Header.Set("Origin", "https://other.example")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}
