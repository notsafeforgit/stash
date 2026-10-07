package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPythonDedupeRecoversDeletionAndPreviewsNextPairAfterRestart(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	db, repo, root, media := fileDeduplicationHTTPFixture(t, []string{"keep.mp4", "duplicate.mp4", "third.mp4"})
	directory := t.TempDir()
	var handlerMu sync.RWMutex
	handler := http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: repo}).router())
	var lost atomic.Bool
	var applies, previews atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handlerMu.RLock()
		handler.ServeHTTP(recorder, r)
		handlerMu.RUnlock()
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/preview") {
			previews.Add(1)
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/apply") {
			applies.Add(1)
			if recorder.Code == http.StatusOK && !lost.Swap(true) {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
				return
			}
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	defer server.Close()
	run := func(phase string) {
		setup, err := json.Marshal(map[string]string{"directory": directory, "endpoint": server.URL,
			"root_uuid": root.UUID, "media": media, "phase": phase})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_dedupe.py"), string(setup))
		command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "PYTHONDONTWRITEBYTECODE=1", "STASH_API_KEY=fixture-application-key")
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	run("lost-response")
	require.True(t, lost.Load())
	require.EqualValues(t, 1, applies.Load())
	require.EqualValues(t, 1, previews.Load())
	require.NoFileExists(t, filepath.Join(media, "duplicate.mp4"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	handlerMu.Lock()
	handler = http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: repo}).router())
	handlerMu.Unlock()
	run("recovered")
	require.EqualValues(t, 2, applies.Load(), "committed first removal must recover without another apply")
	require.EqualValues(t, 2, previews.Load(), "next pair must preview the new primary/owner revision")
	require.NoFileExists(t, filepath.Join(media, "third.mp4"))
	require.FileExists(t, filepath.Join(media, "keep.mp4"))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		scene, err := repo.Scene.Find(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, "Selected title", scene.Title)
		require.NotNil(t, scene.PrimaryFileID)
		primary, err := repo.File.Find(ctx, *scene.PrimaryFileID)
		require.NoError(t, err)
		require.Len(t, primary, 1)
		require.Equal(t, filepath.Join(media, "keep.mp4"), primary[0].Base().Path)
		return nil
	}))
}
