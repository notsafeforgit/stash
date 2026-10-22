package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeMediaRootIdentityUpgrade(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "identity.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	dir := t.TempDir()
	f, err := os.Open(dir)
	require.NoError(t, err)
	legacy, err := fsutil.OpenFileIdentity(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	var root *models.MediaRoot
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{
			Label: "Legacy root", State: "active", Binding: &models.MediaRootBinding{Path: dir, DirectoryIdentity: legacy}}})
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(target string, body any, origin string) *httptest.ResponseRecorder {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := request("/media-roots/probe", map[string]string{"server_path": dir}, "")
	require.Equal(t, http.StatusOK, w.Code)
	var checked models.MediaRootBinding
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &checked))
	input := map[string]any{"expected_revision": root.Revision, "binding": checked, "reason": "Confirmed existing mount after reboot"}
	target := "/media-roots/" + root.UUID + "/identity-upgrade"
	require.Equal(t, http.StatusForbidden, request(target, input, "https://other.example").Code)
	w = request(target, input, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var upgraded models.MediaRoot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &upgraded))
	require.Equal(t, root.Revision, upgraded.Revision)
	require.Equal(t, legacy, upgraded.Binding.DirectoryIdentity)
	require.Equal(t, checked.DirectoryIdentity, upgraded.Binding.PersistentIdentity)
	require.Equal(t, input["reason"], upgraded.IdentityUpgrade.Reason)
	require.JSONEq(t, w.Body.String(), request(target, input, "").Body.String())
	input["expected_revision"] = root.Revision + 1
	require.Equal(t, http.StatusConflict, request(target, input, "").Code)
	input["expected_revision"] = root.Revision
	input["reason"] = ""
	require.Equal(t, http.StatusBadRequest, request(target, input, "").Code)
	input["reason"] = "Confirmed existing mount after reboot"
	require.NoError(t, os.Rename(dir, dir+"-previous"))
	require.NoError(t, os.Mkdir(dir, 0700))
	require.Equal(t, http.StatusBadRequest, request(target, input, "").Code, "even a replay must not accept a replaced mount")
}
