package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonDownloadWorkerRecoversFinishAndDeliversFiles(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	mediaPath := filepath.Join(directory, "media")
	require.NoError(t, os.Mkdir(mediaPath, 0700))
	binding, err := archive.ProbeMediaRoot(mediaPath)
	require.NoError(t, err)
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "library.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	service := ingest.New(db.Repository())
	var producer *models.IngestProducer
	var root *models.MediaRoot
	var collection *models.SourceCollection
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		producer, err = service.Repo.Ingest.CreateProducer(ctx, "Python download fixture")
		if err != nil {
			return err
		}
		root, err = service.Repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{
			Label: "Worker fixture", State: "active", Binding: binding,
		}})
		if err != nil {
			return err
		}
		collection, err = service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Worker feed", Kind: "feed", Namespace: "native:reddit", State: "active", TargetURL: "https://fixture.invalid/account",
			RootUUID: &root.UUID, PathPrefix: "Account",
		}})
		return err
	}))
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	router := (&ingestRoutes{service: service, fileIngestion: true}).router()
	var finishes, finishStatus atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/lease") {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			if bytes.Contains(body, []byte(`"outcome"`)) && finishes.Add(1) == 1 {
				committed := httptest.NewRecorder()
				router.ServeHTTP(committed, r)
				finishStatus.Store(int32(committed.Code))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"error":"temporarily_unavailable"}`)
				return
			}
		}
		router.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	setup, err := json.Marshal(map[string]interface{}{
		"directory": directory, "endpoint": server.URL, "producer": producer.UUID, "root": root.UUID,
		"collection": collection.UUID, "revision": collection.Revision,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests/http_worker_interop.py"))
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_INGEST_TOKEN="+token)
	command.Stdin = bytes.NewReader(setup)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	require.NoError(t, command.Run(), stderr.String())
	var result struct {
		RunUUID string `json:"run_uuid"`
		Capture string `json:"capture"`
		File    string `json:"file"`
		Path    string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.EqualValues(t, http.StatusOK, finishStatus.Load())
	require.EqualValues(t, 1, finishes.Load())
	require.NoError(t, service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		run, err := service.Repo.SourceRun.Find(ctx, result.RunUUID)
		require.NoError(t, err)
		require.Equal(t, "succeeded", run.State)
		require.EqualValues(t, 1, run.Progress.FilesCompleted)
		capture, err := service.Repo.Ingest.FindReceipt(ctx, producer.UUID, result.Capture)
		require.NoError(t, err)
		require.NotNil(t, capture)
		file, err := service.Repo.Ingest.FindReceipt(ctx, producer.UUID, result.File)
		require.NoError(t, err)
		require.NotNil(t, file)
		require.Equal(t, capture.CaptureUUID, file.CaptureUUID)
		require.NotEmpty(t, file.JobUUID)
		registered, err := service.Repo.File.FindByPath(ctx, filepath.Join(mediaPath, result.Path), true)
		require.NoError(t, err)
		require.Nil(t, registered, "source completion and file admission do not certify verified media intake")
		return nil
	}))
	status, err := service.ReceiptStatus(t.Context(), token, result.File)
	require.NoError(t, err)
	require.Equal(t, "queued", status.State)
	require.FileExists(t, filepath.Join(mediaPath, result.Path))
}
