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

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonManualGalleryRegistersRecoversAndQueuesNativeIntake(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	media := filepath.Join(directory, "media")
	require.NoError(t, os.Mkdir(media, 0700))
	binding, err := archive.ProbeMediaRoot(media)
	require.NoError(t, err)
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "library.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	service := ingest.New(db.Repository())
	var root *models.MediaRoot
	var producer *models.IngestProducer
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		root, err = service.Repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Manual root", State: "active", Binding: binding}})
		require.NoError(t, err)
		producer, err = service.Repo.Ingest.CreateProducer(ctx, "Manual fixture")
		return err
	}))
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, nil, nil, root.UUID)
	require.NoError(t, err)
	application := http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: service.Repo}).router())
	producerRoutes := (&ingestRoutes{service: service, fileIngestion: true}).router()
	var policyDropped, finishDropped atomic.Bool
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, ingestPath+"/") {
			if r.Header.Get("ApiKey") != "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/lease") && r.Method == http.MethodPost {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				if bytes.Contains(body, []byte(`"outcome"`)) && !finishDropped.Swap(true) {
					recorded := httptest.NewRecorder()
					producerRoutes.ServeHTTP(recorded, r)
					if recorded.Code != http.StatusOK {
						t.Errorf("manual source completion: %d %s", recorded.Code, recorded.Body.String())
					}
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
			}
			producerRoutes.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorded := httptest.NewRecorder()
		application.ServeHTTP(recorded, r)
		if r.Method == http.MethodPut && recorded.Code == http.StatusOK {
			writes.Add(1)
			if strings.HasSuffix(r.URL.Path, "/metadata-policy") && !policyDropped.Swap(true) {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		for key, values := range recorded.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorded.Code)
		_, _ = w.Write(recorded.Body.Bytes())
	}))
	t.Cleanup(server.Close)
	call := uuid.NewString()
	var result struct {
		Verified   bool   `json:"verified"`
		RunUUID    string `json:"run_uuid"`
		Capture    string `json:"capture"`
		File       string `json:"file"`
		Downloaded string `json:"downloaded"`
	}
	for _, phase := range []string{"lost_policy", "resume"} {
		setup, err := json.Marshal(map[string]any{"directory": directory, "root": root.UUID, "producer": producer.UUID,
			"endpoint": server.URL, "phase": phase, "call": call, "target": "https://fixture.invalid/account"})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests/http_manual_gallery.py"))
		command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key", "STASH_INGEST_TOKEN="+token)
		command.Stdin = bytes.NewReader(setup)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err = command.Run()
		cancel()
		require.NoError(t, err, "stderr: %s\nstdout: %s", stderr.String(), stdout.String())
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
		require.True(t, result.Verified)
		require.EqualValues(t, 2, writes.Load(), "restart preserves the original source and policy")
	}
	require.True(t, policyDropped.Load())
	require.True(t, finishDropped.Load())
	require.NoError(t, service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		run, err := service.Repo.SourceRun.Find(ctx, result.RunUUID)
		require.NoError(t, err)
		require.Equal(t, "succeeded", run.State)
		require.Len(t, run.Completed, 1)
		require.Equal(t, "traversal", run.Completed[0].Basis)
		collection, err := service.Repo.SourceCollection.Find(ctx, run.CollectionUUID)
		require.NoError(t, err)
		require.Empty(t, collection.Namespace)
		require.Nil(t, collection.AccountUUID)
		policy, err := service.Repo.MetadataPolicy.Find(ctx, collection.UUID)
		require.NoError(t, err)
		require.NotNil(t, policy)
		capture, err := service.Repo.Ingest.FindReceipt(ctx, producer.UUID, result.Capture)
		require.NoError(t, err)
		file, err := service.Repo.Ingest.FindReceipt(ctx, producer.UUID, result.File)
		require.NoError(t, err)
		require.NotNil(t, capture)
		require.NotNil(t, file)
		require.Equal(t, capture.CaptureUUID, file.CaptureUUID)
		require.NotEmpty(t, file.JobUUID)
		return nil
	}))
	state, err := service.ReceiptStatus(t.Context(), token, result.File)
	require.NoError(t, err)
	require.Equal(t, "queued", state.State, "manual source completion does not claim verified file intake")
	require.NoError(t, db.Close())
	command := exec.CommandContext(t.Context(), python, "-c", pythonManualGalleryRestore, db.DatabasePath(), filepath.Join(directory, "producer.sqlite"), filepath.Join(directory, "manual", call), t.TempDir(), server.URL)
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(filepath.Dir(packagePath), "archive", "src"))
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	var restored struct {
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(output, &restored))
	_, err = sqlite.VerifyNativeSnapshot(t.Context(), restored.Path)
	require.NoError(t, err)
	require.NoError(t, db.Open(restored.Path))
	retained, err := service.Receipt(t.Context(), token, result.Downloaded)
	require.NoError(t, err)
	require.Equal(t, "attachment.download", retained.Kind)
}

const pythonManualGalleryRestore = `
from pathlib import Path
import json, sys
from stash_archive.bundle import export_archive, import_archive
from stash_archive.receipts import verify_snapshot_receipts
database, outbox, state, destination = map(Path, sys.argv[1:5])
origin = sys.argv[5]
before = verify_snapshot_receipts(database, [outbox], origin)
assert before['producers'][0]['counts']['acknowledged'] == 4, before
files = {path.name: path for path in state.iterdir() if path.is_file()}
assert set(files) == {'request.json', 'sources.json', 'plan.json', 'registered.json'}, files
components = [{'role': 'producer_outbox', 'name': 'manual.sqlite', 'path': outbox}]
components += [{'role': 'operating_state', 'name': 'manual-' + name, 'path': path} for name, path in files.items()]
export_archive(database, destination / 'archive', reserve=0, components=components)
import_archive(destination / 'archive', destination / 'restored', reserve=0)
restored = destination / 'restored' / 'library.sqlite'
queues = list((destination / 'restored').rglob('manual.sqlite'))
assert len(queues) == 1
assert before == verify_snapshot_receipts(restored, queues, origin)
for name, path in files.items():
    copies = list((destination / 'restored').rglob('manual-' + name))
    assert len(copies) == 1 and copies[0].read_bytes() == path.read_bytes()
print(json.dumps({'path': str(restored)}))
`
