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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonAlbumBackfillResumesLostAdmissionCancelAndRetry(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	post := albumHTTPPost(t, repo)
	service := gallery.NewAlbumBackfill(repo)
	router := (&nativeArchiveRoutes{repo: repo, albums: service}).router()
	handler := http.StripPrefix("/api/v3/archive", router)
	var lostApply, lostCancel, lostRetry atomic.Bool
	var applyCalls, cancelCalls, retryCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		drop := false
		if r.Method == http.MethodPost {
			switch {
			case strings.HasSuffix(r.URL.Path, "/album-backfills"):
				applyCalls.Add(1)
				drop = recorder.Code == http.StatusAccepted && !lostApply.Swap(true)
			case strings.HasSuffix(r.URL.Path, "/cancel"):
				cancelCalls.Add(1)
				drop = recorder.Code == http.StatusOK && !lostCancel.Swap(true)
			case strings.HasSuffix(r.URL.Path, "/retry"):
				retryCalls.Add(1)
				drop = recorder.Code == http.StatusAccepted && !lostRetry.Swap(true)
			}
		}
		if drop {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
			return
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	defer server.Close()
	run := func(phase string) {
		setup, err := json.Marshal(map[string]string{"directory": directory, "endpoint": server.URL, "post_uuid": post.UUID, "phase": phase})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_album_backfill.py"), string(setup))
		command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "PYTHONDONTWRITEBYTECODE=1", "STASH_API_KEY=fixture-application-key")
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	run("queued")
	require.True(t, lostApply.Load())
	require.True(t, lostCancel.Load())
	require.True(t, lostRetry.Load())
	history, err := service.History(t.Context(), post.UUID, 0, 100)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, "cancelled", history[0].State)
	require.Equal(t, "queued", history[1].State)
	worker := gallery.NewAlbumWorker(service, func(ctx context.Context, _ gallery.AlbumPublication, guard gallery.AlbumEffectGuard) error {
		return guard(ctx)
	})
	runtime := &archiveWorkerRuntime{worker: worker}
	t.Cleanup(runtime.stop)
	runtime.start()
	require.Eventually(t, func() bool {
		status, err := service.Status(t.Context(), history[1].JobUUID, false)
		return err == nil && status.State == "succeeded" && status.HooksFinished
	}, 10*time.Second, 20*time.Millisecond)
	runtime.stop()
	run("complete")
	require.EqualValues(t, 1, applyCalls.Load())
	require.EqualValues(t, 1, cancelCalls.Load())
	require.EqualValues(t, 1, retryCalls.Load())
	history, err = service.History(t.Context(), post.UUID, 0, 100)
	require.NoError(t, err)
	require.Len(t, history, 2)
	fresh, err := service.Preview(t.Context(), post.UUID, history[1].Policy)
	require.NoError(t, err)
	require.Equal(t, "sync", fresh.Action)
	require.Equal(t, history[1].Publication.GalleryUUID, fresh.Gallery.UUID)
	require.NotEqual(t, history[1].Signature, fresh.Signature)
}
