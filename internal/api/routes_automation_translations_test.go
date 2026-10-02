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
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonAutomationTranslationImportRecoversCommittedResponseAndInspectsReview(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO performers(id,created_at,updated_at) VALUES(71,'2020-01-01 00:00:00','2020-01-01 00:00:00');
 INSERT INTO performer_names(performer_id,name,position) VALUES(71,'Preserved name',0)`, nil)
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
		if recorder.Code == 200 && r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/translation-import") && !lost.Swap(true) {
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
	id := uuid.NewString()
	setup, err := json.Marshal(map[string]any{"directory": directory, "source": uuid.NewString(), "snapshot": id, "endpoint": server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_automation_translations.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, lost.Load())
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	var manifestSHA string
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		progress, err := repo.AutomationTranslationImport.Find(ctx, id)
		require.NoError(t, err)
		require.EqualValues(t, 205, progress.ProcessedRecords)
		require.EqualValues(t, 1, progress.ReviewRecords)
		require.False(t, progress.Imported)
		manifestSHA = progress.ManifestSHA256
		return nil
	}))
	for _, test := range []struct {
		method, suffix, body string
		status               int
	}{
		{"GET", "", "", 200}, {"GET", "/records?limit=101", "", 400}, {"GET", "/records?after=-1", "", 400},
		{"GET", "/records/1", "", 200}, {"GET", "/records/0", "", 400}, {"GET", "/records/9999", "", 404},
		{"POST", "", `{}`, 400}, {"POST", "", `{"after":-1,"expected_manifest_sha256":"bad"}`, 400},
		{"GET", "/held-targets?expected_manifest_sha256=" + manifestSHA, "", 200},
	} {
		r := httptest.NewRequest(test.method, "/automation-snapshots/"+id+"/translation-import"+test.suffix, strings.NewReader(test.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, test.status, w.Code, test.suffix)
	}
	r := httptest.NewRequest("POST", "/automation-snapshots/"+id+"/translation-import", strings.NewReader("{}"))
	r.Header.Set("Origin", "https://unrelated.invalid")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	require.Equal(t, http.StatusForbidden, w.Code)
}
