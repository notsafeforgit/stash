package api

import (
	"bytes"
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
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonAutomationSnapshotUploadResumesAndRejectsUnauthorizedInput(t *testing.T) {
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
	var lostBegin, lostChunk atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		drop := recorder.Code == 200 && ((r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/automation-snapshots") && !lostBegin.Swap(true)) ||
			(r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/chunks/0") && !lostChunk.Swap(true)))
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
	id := uuid.NewString()
	setup, err := json.Marshal(map[string]any{"directory": directory, "source": uuid.NewString(), "snapshot": id, "endpoint": server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_automation_upload.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, lostBegin.Load() && lostChunk.Load())
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		receipt, err := repo.AutomationSnapshot.Find(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "received", receipt.State)
		require.EqualValues(t, 14, receipt.ReceivedRecords)
		require.Len(t, receipt.PendingFamilies, 10)
		require.False(t, receipt.Imported)
		return nil
	}))
	for _, test := range []struct {
		method, path, contentType string
		status                    int
	}{
		{"GET", "/automation-snapshots/" + id, "", 200},
		{"GET", "/automation-snapshots/invalid", "", 400},
		{"GET", "/automation-snapshots/" + uuid.NewString(), "", 404},
		{"POST", "/automation-snapshots", "application/json", 400},
		{"POST", "/automation-snapshots", "text/plain", 400},
		{"PUT", "/automation-snapshots/" + id + "/chunks/-1", "application/x-ndjson", 400},
		{"PUT", "/automation-snapshots/" + id + "/chunks/no", "application/x-ndjson", 400},
		{"PUT", "/automation-snapshots/" + id + "/chunks/0", "application/json", 400},
	} {
		r := httptest.NewRequest(test.method, test.path, strings.NewReader("{}"))
		r.Header.Set("Content-Type", test.contentType)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, test.status, w.Code, test.path)
	}
	body, err := os.ReadFile(filepath.Join(directory, "snapshot", "manifest.json"))
	require.NoError(t, err)
	for _, encoding := range []string{"gzip", "oversize"} {
		input := body
		if encoding == "oversize" {
			input = bytes.Repeat([]byte{' '}, scrape.CatalogManifestLimit+1)
		}
		r := httptest.NewRequest("POST", "/automation-snapshots", bytes.NewReader(input))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Stash-Manifest-SHA256", scrape.CatalogSnapshotSHA(input))
		if encoding == "gzip" {
			r.Header.Set("Content-Encoding", "gzip")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, 400, w.Code)
	}
	csrf := httptest.NewRequest("POST", "/automation-snapshots", bytes.NewReader(body))
	csrf.Header.Set("Origin", "https://unrelated.invalid")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, csrf)
	require.Equal(t, http.StatusForbidden, w.Code)
	var producer *models.IngestProducer
	var collection *models.SourceCollection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = repo.Ingest.CreateProducer(ctx, "Scoped producer")
		if err != nil {
			return err
		}
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Feed", Kind: "feed", State: "active"}})
		return err
	}))
	intake := ingest.New(repo)
	_, token, err := intake.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID}}, nil)
	require.NoError(t, err)
	producerHandler := withIngestRoutes(http.NotFoundHandler(), intake, false)
	for _, prefix := range []string{ingestPath, "/api/v3/archive"} {
		r := httptest.NewRequest("POST", prefix+"/automation-snapshots", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		producerHandler.ServeHTTP(w, r)
		require.Equal(t, http.StatusNotFound, w.Code)
	}
}
