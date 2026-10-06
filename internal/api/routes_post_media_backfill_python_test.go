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

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestPythonPostMediaBackfillResumesCommittedLinkAfterRestart(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	db, repo, post := postMediaBackfillHTTPFixture(t)
	directory := t.TempDir()
	var handlerMu sync.RWMutex
	handler := http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: repo}).router())
	var lost atomic.Bool
	var applies atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handlerMu.RLock()
		handler.ServeHTTP(recorder, r)
		handlerMu.RUnlock()
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/media-backfills") {
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
		setup, err := json.Marshal(map[string]string{"directory": directory, "endpoint": server.URL, "post_uuid": post, "phase": phase})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_post_media_backfill.py"), string(setup))
		command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "PYTHONDONTWRITEBYTECODE=1", "STASH_API_KEY=fixture-application-key")
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	run("lost-response")
	require.True(t, lost.Load())
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	var media string
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		entity, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveScene, 1)
		if err != nil {
			return err
		}
		media = entity.UUID
		association, err := repo.SourcePostMedia.Association(ctx, post, media)
		if err != nil {
			return err
		}
		_, err = repo.SourcePostMedia.Decide(ctx, models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: post, MediaUUID: media,
			ExpectedPostRevision: association.PostRevision, ExpectedMediaRevision: association.MediaRevision,
			ExpectedDecisions: []string{association.Decisions[0].UUID}, State: "unlinked", Origin: "review", Reason: "Later review"})
		return err
	}))
	handlerMu.Lock()
	handler = http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: repo}).router())
	handlerMu.Unlock()
	run("recovered")
	require.EqualValues(t, 1, applies.Load())
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		association, err := repo.SourcePostMedia.Association(ctx, post, media)
		require.NoError(t, err)
		require.Equal(t, "unlinked", association.State)
		return nil
	}))
}
