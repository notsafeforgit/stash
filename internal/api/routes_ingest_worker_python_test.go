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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPythonDownloadWorkerRecoversFinishAndDeliversFiles(t *testing.T) {
	for _, adapter := range []string{"caller-cli", "host-launcher", "n8n-backfill", "ytdl", "ytdl-traversal", "tumblr", "jpgfish", "leakgallery"} {
		t.Run(adapter, func(t *testing.T) { runPythonDownloadWorker(t, adapter) })
	}
}

func runPythonDownloadWorker(t *testing.T, adapter string) {
	t.Helper()
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
	target := "https://fixture.invalid/account"
	namespace, attachmentKey := "native:reddit", "abc123"
	switch adapter {
	case "tumblr":
		target, namespace, attachmentKey = "https://example.tumblr.com/", "native:tumblr", "url:b5ad9b7ba3b6d5f2029eb0cc302d9b4ca17c9fe3391c3926edb4acb149971450"
	case "jpgfish":
		target, namespace, attachmentKey = "https://jpg7.cr/img/Photo.AbCd", "native:jpgfish", "url:109590cc64a1ec6e2c80bd7eb311e80d5cfbba72613fa6bfef6a132be7650252"
	case "leakgallery":
		target, namespace, attachmentKey = "https://leakgallery.com/creator/123", "native:leakgallery", "url:31878403fea8221d5bdaa314c1690ec0af58224386d380b9c9f76ef3c92b36f7"
	case "host-launcher":
		target = "https://www.reddit.com/r/native_fixture/?sort=new"
	case "n8n-backfill":
		target = "https://www.reddit.com/user/Native_Fixture/submitted/?sort=new&t=all"
	case "ytdl", "ytdl-traversal":
		target, namespace, attachmentKey = "https://fixture.invalid/video/one", "ytdl:nativevideofixture", "one"
	}
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
			Label: "Worker feed", Kind: "feed", Namespace: namespace, State: "active", TargetURL: target,
			RootUUID: &root.UUID, PathPrefix: "Account",
		}})
		return err
	}))
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, nil, nil, root.UUID)
	require.NoError(t, err)
	router := (&ingestRoutes{service: service, fileIngestion: true}).router()
	var finishes, finishStatus atomic.Int32
	var reportResponses atomic.Int32
	var backfillFinishes atomic.Int32
	var firstProof atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/batches") {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			if bytes.Contains(body, []byte(`"attachment.download"`)) && reportResponses.Add(1) == 1 {
				committed := httptest.NewRecorder()
				router.ServeHTTP(committed, r)
				assert.Equal(t, http.StatusOK, committed.Code, committed.Body.String())
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/backfills/complete") {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			if backfillFinishes.Add(1) == 1 {
				firstProof.Store(body)
				committed := httptest.NewRecorder()
				router.ServeHTTP(committed, r)
				assert.Equal(t, http.StatusOK, committed.Code, committed.Body.String())
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			assert.Equal(t, firstProof.Load(), body, "lost completion response must replay the exact original proof")
		}
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
		"target": target, "adapter": adapter,
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
		RunUUID    string `json:"run_uuid"`
		Capture    string `json:"capture"`
		File       string `json:"file"`
		Path       string `json:"path"`
		Started    string `json:"started"`
		Downloaded string `json:"downloaded"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.EqualValues(t, http.StatusOK, finishStatus.Load())
	require.EqualValues(t, 1, finishes.Load())
	if adapter == "n8n-backfill" {
		require.EqualValues(t, 2, backfillFinishes.Load())
	}
	var attachmentID string
	require.NoError(t, service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		run, err := service.Repo.SourceRun.Find(ctx, result.RunUUID)
		require.NoError(t, err)
		require.Equal(t, "succeeded", run.State)
		require.EqualValues(t, 1, run.Progress.FilesCompleted)
		capture, err := service.Repo.Ingest.FindReceipt(ctx, producer.UUID, result.Capture)
		require.NoError(t, err)
		require.NotNil(t, capture)
		if adapter == "ytdl-traversal" || adapter == "jpgfish" || adapter == "leakgallery" {
			require.Len(t, run.Completed, 1)
			require.Equal(t, "traversal", run.Completed[0].Basis)
			observed, err := service.Repo.SourceEvidence.FindCapture(ctx, capture.CaptureUUID)
			require.NoError(t, err)
			require.Nil(t, observed.Metadata.PublishedAt, "scan request time must never become a publication date")
		}
		file, err := service.Repo.Ingest.FindReceipt(ctx, producer.UUID, result.File)
		require.NoError(t, err)
		require.NotNil(t, file)
		require.Equal(t, capture.CaptureUUID, file.CaptureUUID)
		require.NotEmpty(t, file.JobUUID)
		attachment, err := service.Repo.SourceAttachment.Lookup(ctx, capture.PostUUID, models.SourcePostIdentifier{Namespace: namespace, Value: attachmentKey})
		require.NoError(t, err)
		require.NotNil(t, attachment)
		attachmentID = attachment.UUID
		history, err := service.Repo.SourceAttachment.DownloadHistory(ctx, attachment.UUID, 0, 10, time.Now())
		require.NoError(t, err)
		require.Len(t, history, 2)
		for _, report := range history {
			require.Equal(t, "downloaded", report.TransferState)
			require.Equal(t, "queued", report.VerificationState)
		}
		for event, state := range map[string]string{result.Started: "started", result.Downloaded: "downloaded"} {
			receipt, err := service.Repo.Ingest.FindReceipt(ctx, producer.UUID, event)
			require.NoError(t, err)
			require.NotNil(t, receipt)
			require.Equal(t, capture.CaptureUUID, receipt.CaptureUUID)
			require.Empty(t, receipt.JobUUID)
			require.JSONEq(t, `{"status":"recorded","attachment_uuid":"`+attachment.UUID+`","reported_state":"`+state+`","media_ingested":false}`, string(receipt.Result))
		}
		registered, err := service.Repo.File.FindByPath(ctx, filepath.Join(mediaPath, result.Path), true)
		require.NoError(t, err)
		require.Nil(t, registered, "source completion and file admission do not certify verified media intake")
		if adapter == "n8n-backfill" {
			backfill, err := service.Repo.SourceBackfill.Status(ctx, models.BackfillSubject{RootUUID: root.UUID, Platform: "reddit", Account: "native_fixture"}, "reddit-profile-new")
			require.NoError(t, err)
			require.Equal(t, "completed", backfill.State)
			require.False(t, backfill.AccountComplete)
			require.Len(t, backfill.Decisions, 1)
			require.Equal(t, "source_runs", backfill.Decisions[0].Basis)
		}
		return nil
	}))
	status, err := service.ReceiptStatus(t.Context(), token, result.File)
	require.NoError(t, err)
	require.Equal(t, "queued", status.State)
	require.FileExists(t, filepath.Join(mediaPath, result.Path))
	verifyAttachmentDownloadTransferHTTP(t, service.Repo, attachmentID, result.Started, result.Downloaded, result.File)
	require.Positive(t, reportResponses.Load(), "a committed report response was deliberately lost")
	require.NoError(t, db.Close())
	archivePath := filepath.Join(filepath.Dir(packagePath), "archive", "src")
	command = exec.CommandContext(ctx, python, "-c", pythonDownloadReceiptRestore, db.DatabasePath(), filepath.Join(directory, "producer.sqlite"), t.TempDir(), server.URL)
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+archivePath)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	var restored struct {
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(output, &restored))
	_, err = sqlite.VerifyNativeSnapshot(t.Context(), restored.Path)
	require.NoError(t, err)
	require.NoError(t, db.Open(restored.Path))
	replayed, err := service.Receipt(t.Context(), token, result.Downloaded)
	require.NoError(t, err)
	require.Equal(t, "attachment.download", replayed.Kind)
}

const pythonDownloadReceiptRestore = `
from pathlib import Path
import json, sys
from stash_archive.bundle import export_archive, import_archive
from stash_archive.receipts import verify_snapshot_receipts
database, outbox, destination = map(Path, sys.argv[1:4])
origin = sys.argv[4]
before = verify_snapshot_receipts(database, [outbox], origin)
assert before['producers'][0]['counts']['acknowledged'] == 4, before
export_archive(database, destination / 'archive', reserve=0,
               components=[{'role': 'producer_outbox', 'name': 'download.sqlite', 'path': outbox}])
import_archive(destination / 'archive', destination / 'restored', reserve=0)
restored = destination / 'restored' / 'library.sqlite'
queues = list((destination / 'restored').rglob('download.sqlite'))
assert len(queues) == 1, queues
after = verify_snapshot_receipts(restored, queues, origin)
assert before == after
print(json.dumps({'path': str(restored)}))
`
