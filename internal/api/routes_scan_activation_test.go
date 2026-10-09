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
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonScanActivationKeepsDeferralAfterLostResponse(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	now := time.Now().UTC().Truncate(time.Millisecond)
	binding := models.ScanJournalActivationInput{UUID: uuid.NewString(), PolicySHA256: strings.Repeat("a", 64), Cutoff: now}
	rootBinding, err := archive.ProbeMediaRoot(t.TempDir())
	require.NoError(t, err)
	document, err := os.ReadFile("../../pkg/scrape/testdata/legacy_scan_journal.json")
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal(document, &data))
	data["captured_at"] = now.Add(-time.Minute).Format(time.RFC3339Nano)
	data["tables"].(map[string]any)["scan_jobs"].([]any)[0].(map[string]any)["retry_after"] = 0
	document, err = json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		root, err := repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "migration", MediaRootDefinition: models.MediaRootDefinition{Label: "Fixture", State: "active", Binding: rootBinding}})
		if err != nil {
			return err
		}
		collection, err := repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Fixture", Kind: "feed", Namespace: "native:reddit", State: "active", RootUUID: &root.UUID, PathPrefix: ".",
			TargetURL: "https://www.reddit.com/user/Example/submitted/?sort=new",
		}})
		if err != nil {
			return err
		}
		binding.CollectionUUID, binding.CollectionRevision, binding.RootRevision = collection.UUID, collection.Revision, root.Revision
		journal, err := repo.ScanJournal.Import(ctx, models.ScanJournalInput{UUID: uuid.NewString(), SourceUUID: uuid.NewString(), RootUUID: root.UUID, Document: document}, now)
		if err != nil {
			return err
		}
		rows, err := repo.ScanJournal.Records(ctx, journal.UUID, "", 0, 100)
		for _, row := range rows {
			if row.Table == "scan_jobs" {
				binding.ScanRecordUUID = row.UUID
			}
			if row.Table == "extractor_jobs" && row.TargetURL == "https://www.reddit.com/user/Example/submitted/?sort=new" {
				binding.CheckpointRecordUUID = row.UUID
			}
		}
		return err
	}))
	router := (&nativeArchiveRoutes{repo: repo}).router()
	handler := http.StripPrefix("/api/v3/archive", router)
	var dropped atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		if r.Method == "POST" && r.URL.Path == "/api/v3/archive/scan-journal-activations" && recorder.Code == http.StatusOK && !dropped.Swap(true) {
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
	setup, err := json.Marshal(map[string]any{"directory": directory, "binding": binding, "endpoint": server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_scan_activation.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, dropped.Load())
	var activation *models.ScanJournalActivation
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		activation, err = repo.ScanJournal.Activation(ctx, binding.UUID)
		if err != nil {
			return err
		}
		run, err := repo.SourceRun.Find(ctx, activation.RunUUID)
		if err != nil {
			return err
		}
		require.Equal(t, "deferred", run.State)
		require.Zero(t, run.Fence)
		attempts, err := repo.SourceRun.Attempts(ctx, run.UUID, 0, 100)
		require.Empty(t, attempts)
		return err
	}))
	service := ingest.New(repo)
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = repo.Ingest.CreateProducer(ctx, "Recovery fixture")
		if err != nil {
			return err
		}
		run, err := repo.SourceRun.Find(ctx, activation.RunUUID)
		if err != nil {
			return err
		}
		_, err = repo.SourceRun.Review(ctx, run.UUID, run.Revision, "retry", time.Now())
		return err
	}))
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: binding.CollectionUUID, RootUUID: &activation.RootUUID}}, nil)
	require.NoError(t, err)
	ingestRouter := (&ingestRoutes{service: service}).router()
	claim := map[string]any{"owner_uuid": uuid.NewString(), "policy_sha256": binding.PolicySHA256, "lease_seconds": 60}
	for _, protocol := range []int{0, 1} {
		if protocol > 0 {
			claim["recovery_protocol"] = protocol
		}
		body, err := json.Marshal(claim)
		require.NoError(t, err)
		request := httptest.NewRequest("POST", ingestPath+"/runs/"+activation.RunUUID+"/claim", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		ingestRouter.ServeHTTP(w, request)
		if protocol == 0 {
			require.Equal(t, http.StatusConflict, w.Code, "old workers must not ignore recovery replay policy: %s", w.Body.String())
		} else {
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var run models.SourceRun
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &run))
			require.Equal(t, int64(1), run.Fence)
			require.Equal(t, activation.Progress, run.Progress)
		}
	}
	for _, path := range []string{"/scan-journal-activations/preview", "/scan-journal-activations"} {
		request := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		request.Header.Set("Origin", "https://other.invalid")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		require.Equal(t, http.StatusForbidden, w.Code)
		w = httptest.NewRecorder()
		(&ingestRoutes{service: ingest.New(repo)}).router().ServeHTTP(w, httptest.NewRequest("POST", ingestPath+path, strings.NewReader(`{}`)))
		require.Equal(t, http.StatusNotFound, w.Code)
	}
}
