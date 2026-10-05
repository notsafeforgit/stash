package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeCollectionManagement(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "collections.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	routes := &nativeArchiveRoutes{repo: db.Repository()}
	handler := routes.router()
	request := func(method, target string, body interface{}) *httptest.ResponseRecorder {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, target, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	rootID := "10000000-0000-4000-8000-000000000001"
	w := request(http.MethodPost, "/media-roots/probe", map[string]interface{}{"server_path": t.TempDir()})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var binding models.MediaRootBinding
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &binding))
	w = request(http.MethodPut, "/media-roots/"+rootID, map[string]interface{}{"label": "Archive %_", "state": "active", "binding": binding})
	require.Equal(t, 200, w.Code, w.Body.String())
	var root models.MediaRoot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &root))
	w = request(http.MethodGet, "/media-roots/"+rootID, nil)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var found models.MediaRoot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &found))
	require.Equal(t, root, found)
	for i := 1; i <= 52; i++ {
		input := models.SourceCollectionInput{SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: fmt.Sprintf("Purchased %02d", i), Kind: "directory", State: "active", RootUUID: &rootID, PathPrefix: fmt.Sprintf("Purchased/%02d", i),
		}, Reason: "Create batch"}
		if i == 1 {
			input.Label = "Literal %_"
		}
		if i == 2 {
			input.State = "disabled"
		}
		if i == 3 {
			input.Kind = "manual_batch"
		}
		id := fmt.Sprintf("20000000-0000-4000-8000-%012d", i)
		w = request(http.MethodPut, "/collections/"+id, input)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	read := func(path string) []models.SourceCollection {
		w := request(http.MethodGet, path, nil)
		require.Equal(t, 200, w.Code, w.Body.String())
		var rows []models.SourceCollection
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rows))
		return rows
	}
	first := read("/collections")
	require.Len(t, first, 50, "retain the default page size for existing callers")
	second := read("/collections?after=" + first[49].UUID)
	require.Len(t, second, 2)
	require.Greater(t, second[0].UUID, first[49].UUID)
	require.Len(t, read("/collections?limit=1&state=disabled"), 1)
	require.Len(t, read("/collections?kind=manual_batch"), 1)
	literal := read("/collections?q=%25_")
	require.Len(t, literal, 1)
	require.Equal(t, "Literal %_", literal[0].Label)
	w = request(http.MethodGet, "/media-roots?q=%25_&limit=1", nil)
	require.Equal(t, 200, w.Code)
	var roots []models.MediaRoot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &roots))
	require.Len(t, roots, 1)
	before := literal[0]
	input := models.SourceCollectionInput{ExpectedRevision: 1, SourceCollectionDefinition: before.SourceCollectionDefinition, Reason: "Rename"}
	input.Label = "New name"
	w = request(http.MethodPut, "/collections/"+before.UUID, input)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 409, request(http.MethodPut, "/collections/"+before.UUID, input).Code)
	require.Empty(t, read("/collections?q=%25_"), "search excludes old labels")
	require.Len(t, read("/collections?q="+before.UUID), 1)
	w = request(http.MethodGet, "/collections/"+before.UUID+"/history?after=1&limit=1", nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	var history []models.SourceCollectionRevision
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.Len(t, history, 1)
	require.Equal(t, 2, history[0].Revision)
	require.Equal(t, "Rename", history[0].Reason)
	require.Equal(t, "review", history[0].Origin)
	for _, path := range []string{"/collections?limit=101", "/collections?state=unknown", "/collections?kind=unknown", "/collections?after=bad", "/collections?q=%00", "/media-roots?kind=directory", "/media-roots/bad", "/collections/" + before.UUID + "/history?after=-1"} {
		require.Equal(t, 400, request(http.MethodGet, path, nil).Code, path)
	}
	require.Equal(t, 404, request(http.MethodGet, "/media-roots/20000000-0000-4000-8000-000000000099", nil).Code)
	require.Equal(t, 404, request(http.MethodGet, "/collections/20000000-0000-4000-8000-000000000099/history", nil).Code)
	missing := "20000000-0000-4000-8000-000000000099"
	for _, definition := range []models.SourceCollectionDefinition{
		{Label: "Missing root", Kind: "directory", State: "active", RootUUID: &missing, PathPrefix: "."},
		{Label: "Missing account", Kind: "account", State: "active", AccountUUID: &missing, Namespace: "native:reddit"},
		{Label: "Bad kind", Kind: "unknown", State: "active"},
	} {
		require.Equal(t, 400, request(http.MethodPut, "/collections/"+missing, models.SourceCollectionInput{SourceCollectionDefinition: definition}).Code)
		require.Equal(t, 404, request(http.MethodGet, "/collections/"+missing, nil).Code, "invalid creation leaves no incomplete identity")
	}
}
