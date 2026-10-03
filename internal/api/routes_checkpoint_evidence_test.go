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

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestCheckpointEvidenceHTTPRecoversCommittedResponseAndKeepsReviewHold(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	source, snapshot := uuid.NewString(), uuid.NewString()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if _, _, err := db.ExecSQL(ctx, "INSERT INTO performers(id,created_at,updated_at) VALUES(71,'2020-01-01 00:00:00','2020-01-01 00:00:00'); INSERT INTO performer_names(performer_id,name,position) VALUES(71,'Preserved name',0)", nil); err != nil {
			return err
		}
		ref, err := scrape.CatalogLocalPostReference(source, "c_"+strings.Repeat("1", 32), "reddit:post:saved")
		if err != nil {
			return err
		}
		_, err = repo.SourceEvidence.EnsurePost(ctx, ref, "")
		return err
	}))
	router := (&nativeArchiveRoutes{repo: repo}).router()
	handler := http.StripPrefix("/api/v3/archive", router)
	var lost atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		if recorder.Code == 200 && r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/checkpoint-evidence") && !lost.Swap(true) {
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
	setup, err := json.Marshal(map[string]any{"directory": directory, "source": source, "snapshot": snapshot, "endpoint": server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_checkpoint_evidence.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, lost.Load())
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	for _, test := range []struct {
		method, path, body, origin string
		status                     int
	}{
		{"POST", "/checkpoint-evidence/preview", "{}", "", 400},
		{"POST", "/checkpoint-evidence", "{}", "", 400},
		{"POST", "/checkpoint-evidence/preview", strings.Repeat(" ", 16385), "", 400},
		{"GET", "/checkpoint-evidence/invalid", "", "", 400},
		{"GET", "/checkpoint-evidence/" + uuid.NewString(), "", "", 404},
		{"POST", "/checkpoint-evidence", "{}", "https://unrelated.invalid", 403},
	} {
		r := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", test.origin)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, test.status, w.Code, test.path)
	}
}
