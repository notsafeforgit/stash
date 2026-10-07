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
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/dedup"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestFileDeduplicationHTTPPreviewApplyReplayAndScope(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "dedupe-http.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	dir := t.TempDir()
	binding, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	var root *models.MediaRoot
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Files", State: "active", Binding: binding}})
		if err != nil {
			return err
		}
		folder, err := file.GetOrCreateFolderHierarchy(ctx, repo.Folder, dir, []string{dir})
		if err != nil {
			return err
		}
		scene := models.Scene{Title: "Selected title", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := repo.Scene.Create(ctx, &scene, nil); err != nil {
			return err
		}
		for _, name := range []string{"keep.mp4", "duplicate.mp4"} {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte("same complete bytes"), 0600); err != nil {
				return err
			}
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			f := &models.VideoFile{BaseFile: &models.BaseFile{Path: path, Basename: name, ParentFolderID: folder.ID,
				Size: info.Size(), DirEntry: models.DirEntry{ModTime: info.ModTime()}, CreatedAt: time.Now(), UpdatedAt: time.Now()}, Width: 4, Height: 3, VideoCodec: "h264"}
			if err := repo.File.Create(ctx, f); err != nil {
				return err
			}
			if err := repo.Scene.AddFileID(ctx, scene.ID, f.ID); err != nil {
				return err
			}
			if name == "duplicate.mp4" {
				_, err = repo.Scene.UpdatePartial(ctx, scene.ID, models.ScenePartial{PrimaryFileID: &f.ID})
				if err != nil {
					return err
				}
			}
		}
		return nil
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(method, path string, body any, origin string) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	input := models.FileDeduplicationInput{RootUUID: root.UUID, KeepPath: "keep.mp4", RemovePath: "duplicate.mp4"}
	w := request(http.MethodPost, "/file-deduplication/preview", input, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NotContains(t, w.Body.String(), dir)
	require.NotContains(t, w.Body.String(), "change_token")
	var preview dedup.Preview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	require.True(t, preview.Eligible)
	apply := dedup.Request{FileDeduplicationInput: input, RequestUUID: uuid.NewString(), Signature: preview.Signature}
	stale := apply
	stale.Signature = "0000000000000000000000000000000000000000000000000000000000000000"
	w = request(http.MethodPost, "/file-deduplication/apply", stale, "")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	w = request(http.MethodPost, "/file-deduplication/apply", apply, "https://unrelated.invalid")
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.FileExists(t, filepath.Join(dir, "duplicate.mp4"))
	w = request(http.MethodPost, "/file-deduplication/apply", apply, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "proof")
	require.NotContains(t, w.Body.String(), dir)
	committed := w.Body.String()
	w = request(http.MethodPost, "/file-deduplication/apply", apply, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, committed, w.Body.String())
	w = request(http.MethodGet, "/file-deduplication/requests/"+apply.RequestUUID, nil, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, committed, w.Body.String())
	w = request(http.MethodGet, "/file-deduplication/requests/"+uuid.NewString(), nil, "")
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	input.RemovePath = "../escape"
	w = request(http.MethodPost, "/file-deduplication/preview", input, "")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.NoFileExists(t, filepath.Join(dir, "duplicate.mp4"))
	require.FileExists(t, filepath.Join(dir, "keep.mp4"))
}
